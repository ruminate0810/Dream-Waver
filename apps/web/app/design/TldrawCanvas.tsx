"use client";

import { useCallback, useEffect, useRef } from "react";
import {
  AssetRecordType,
  createShapeId,
  getHashForString,
  Tldraw,
  type Editor,
  type TLImageShape,
  type TLShapeId,
} from "tldraw";
import "tldraw/tldraw.css";

// TldrawCanvas is the SSR-poison side of the design page — it imports
// `tldraw` and `tldraw/tldraw.css`, both of which touch `window` at
// module load. The parent page lazy-loads this via `next/dynamic` with
// `ssr: false` so a server render never executes this file.
//
// Exposes two channels to the parent:
//
//   onReady(controller)        Fires once after mount. The controller
//                              is the imperative API the AI panel + the
//                              selection toolbar use to place / enhance
//                              / remove-bg images on the canvas. Stable
//                              identity across re-renders — the parent
//                              stashes it in a ref.
//
//   onSelectionChange(info)    Fires whenever the user's selection on
//                              the canvas changes to / from an image
//                              shape with an AI-generated source URL.
//                              Drives the floating SelectionToolbar.
//
// Splitting placeImage out of the parent (it used to be a single
// callback in the first MVP) lets us add new canvas operations —
// placeVariants, replaceSelectedImage — without re-shaping the prop
// contract every time.

/** Info about the currently-selected image shape, surfaced to the parent
 *  so the selection toolbar can act on it. `null` when the selection is
 *  empty / multi / a non-image shape. */
export type SelectedImageInfo = {
  shapeId: string;
  /** The on-canvas image URL (i.e. the asset's `src`). */
  url: string;
  /** Native dimensions from the asset record (NOT the display size). */
  width: number;
  height: number;
  /** Display position (top-left in page-space) for the floating toolbar. */
  pageX: number;
  pageY: number;
  /** Display dimensions on canvas. */
  displayW: number;
  displayH: number;
};

/** Imperative API the parent uses to drive the canvas. */
export type CanvasController = {
  /** Drop one image at viewport centre, half native resolution. */
  placeImage: (url: string, width: number, height: number) => void;
  /** Drop N images in a 2x ceil(N/2) grid centred on viewport. */
  placeVariants: (
    variants: Array<{ url: string; width: number; height: number }>,
  ) => void;
  /** Place a new image immediately to the right of the currently-selected
   *  shape (used by remove-bg / enhance flows so the result sits next to
   *  the source). Falls back to viewport centre when nothing's selected. */
  placeNextToSelection: (
    url: string,
    width: number,
    height: number,
  ) => void;
  /** Drop a dashed-outline placeholder shape at viewport centre and
   *  return its shape id. The placeholder shows "Generating · 3s\n
   *  <prompt>" — text is mutated via updatePlaceholderProgress as SSE
   *  events stream. width/height set the placeholder's display size
   *  so the eventual replaceWithImage call doesn't visibly resize. */
  placePlaceholder: (prompt: string, width: number, height: number) => string;
  /** Update a placeholder's caption to reflect SSE progress. Safe to
   *  call repeatedly; no-op if the shape was deleted. */
  updatePlaceholderProgress: (shapeId: string, elapsedS: number, statusLabel: string) => void;
  /** Replace a placeholder with the real image at the same x/y/w/h. */
  replacePlaceholderWithImage: (
    shapeId: string,
    url: string,
    nativeWidth: number,
    nativeHeight: number,
  ) => void;
  /** Mark a placeholder as failed — turn it red, show the error message
   *  in the caption. User can click to delete + retry from the panel. */
  failPlaceholder: (shapeId: string, message: string) => void;
  /** Select a shape and pan/zoom the viewport so it's centred. Used by
   *  the ChatCopilot history list to jump to a previously-generated
   *  image. No-op if the shape no longer exists. */
  focusShape: (shapeId: string) => void;
};

export type TldrawCanvasProps = {
  onReady: (controller: CanvasController) => void;
  onSelectionChange?: (info: SelectedImageInfo | null) => void;
};

export function TldrawCanvas({ onReady, onSelectionChange }: TldrawCanvasProps) {
  const editorRef = useRef<Editor | null>(null);
  // selectionCallback held in a ref so the store-listen subscription
  // (which captures it at install time) sees the latest version even
  // when the parent passes a new function on re-render.
  const selectionCallbackRef = useRef(onSelectionChange);
  useEffect(() => {
    selectionCallbackRef.current = onSelectionChange;
  }, [onSelectionChange]);

  const handleMount = useCallback(
    (editor: Editor) => {
      editorRef.current = editor;

      const controller: CanvasController = {
        placeImage: (url, w, h) => placeImageOnCanvas(editor, url, w, h),
        placeVariants: (variants) => placeVariantsOnCanvas(editor, variants),
        placeNextToSelection: (url, w, h) =>
          placeImageNextToSelection(editor, url, w, h),
        placePlaceholder: (prompt, w, h) =>
          placePlaceholderOnCanvas(editor, prompt, w, h),
        updatePlaceholderProgress: (id, elapsed, label) =>
          updatePlaceholderProgress(editor, id, elapsed, label),
        replacePlaceholderWithImage: (id, url, w, h) =>
          replacePlaceholderWithImage(editor, id, url, w, h),
        failPlaceholder: (id, msg) => failPlaceholder(editor, id, msg),
        focusShape: (id) => focusShape(editor, id),
      };
      onReady(controller);

      // Selection bridge — fire onSelectionChange whenever the user's
      // selection lands on / leaves an image shape. `editor.store.listen`
      // delivers EVERY store mutation, including drags and resizes, so
      // we always send the freshest bounds for the floating toolbar.
      const dispose = editor.store.listen(
        () => {
          const cb = selectionCallbackRef.current;
          if (!cb) return;
          cb(readSelectedImage(editor));
        },
        // `user` scope skips programmatic mutations from createShape /
        // updateShape we issue ourselves — those would fire a redundant
        // "selection didn't change" callback otherwise.
        { source: "user", scope: "all" },
      );
      // Also fire once on mount so the toolbar reflects any pre-restored
      // selection (TLDraw persists session state in localStorage).
      const cb = selectionCallbackRef.current;
      if (cb) cb(readSelectedImage(editor));

      return () => dispose();
    },
    [onReady],
  );

  return <Tldraw onMount={handleMount} />;
}

// ─── Read selected image ─────────────────────────────────────────────

function readSelectedImage(editor: Editor): SelectedImageInfo | null {
  const selected = editor.getSelectedShapes();
  if (selected.length !== 1) return null;
  const shape = selected[0];
  if (shape.type !== "image") return null;

  const img = shape as TLImageShape;
  const assetId = img.props.assetId;
  if (!assetId) return null;
  const asset = editor.getAsset(assetId);
  if (!asset || asset.type !== "image") return null;
  const src = asset.props.src;
  if (!src) return null;
  // Restrict the toolbar to images that came through our AI pipeline.
  // External / pasted images don't have a known URL the sidecar can
  // fetch, so showing edit actions for them would 422 on submit.
  if (asset.props.name !== "ai-image") return null;

  return {
    shapeId: img.id,
    url: src,
    width: asset.props.w,
    height: asset.props.h,
    pageX: img.x,
    pageY: img.y,
    displayW: img.props.w,
    displayH: img.props.h,
  };
}

// ─── Place operations ────────────────────────────────────────────────

/**
 * placeImageOnCanvas drops a generated image at the centre of the
 * current viewport at half its native resolution — that keeps a 1024px
 * image comfortably visible in the typical 1440-wide canvas instead of
 * dominating the entire frame.
 */
function placeImageOnCanvas(
  editor: Editor,
  url: string,
  width: number,
  height: number,
) {
  const assetId = ensureAsset(editor, url, width, height);
  const bounds = editor.getViewportPageBounds();
  const displayW = width / 2;
  const displayH = height / 2;
  editor.createShape({
    type: "image",
    x: bounds.midX - displayW / 2,
    y: bounds.midY - displayH / 2,
    props: { assetId, w: displayW, h: displayH },
  });
}

/**
 * placeVariantsOnCanvas lays N images out in a 2-column grid centred on
 * the viewport. Spacing between images is 16 px in page-space, same in
 * both axes. Variants are typically 1024² so each cell renders at 512²
 * for a 2x2 grid that fits comfortably in a single viewport.
 */
function placeVariantsOnCanvas(
  editor: Editor,
  variants: Array<{ url: string; width: number; height: number }>,
) {
  if (variants.length === 0) return;
  const cols = 2;
  const rows = Math.ceil(variants.length / cols);
  const gap = 16;
  // Each cell uses half of the first variant's native resolution so the
  // grid is uniform even if (somehow) variants returned different sizes.
  const cellW = variants[0].width / 2;
  const cellH = variants[0].height / 2;
  const totalW = cols * cellW + (cols - 1) * gap;
  const totalH = rows * cellH + (rows - 1) * gap;
  const bounds = editor.getViewportPageBounds();
  const originX = bounds.midX - totalW / 2;
  const originY = bounds.midY - totalH / 2;

  for (let i = 0; i < variants.length; i++) {
    const v = variants[i];
    const r = Math.floor(i / cols);
    const c = i % cols;
    const assetId = ensureAsset(editor, v.url, v.width, v.height);
    editor.createShape({
      type: "image",
      x: originX + c * (cellW + gap),
      y: originY + r * (cellH + gap),
      props: { assetId, w: cellW, h: cellH },
    });
  }
}

/**
 * placeImageNextToSelection drops `url` immediately to the right of the
 * currently-selected shape with a small visual gap, so a remove-bg /
 * enhance result sits where the user expects (right of source). Falls
 * back to viewport centre when nothing is selected.
 */
function placeImageNextToSelection(
  editor: Editor,
  url: string,
  width: number,
  height: number,
) {
  const selected = editor.getSelectedShapes();
  if (selected.length !== 1 || selected[0].type !== "image") {
    placeImageOnCanvas(editor, url, width, height);
    return;
  }
  const src = selected[0] as TLImageShape;
  // Match the source's display size so before/after sit visually
  // matched even when the edit op upscaled the underlying asset.
  const displayW = src.props.w;
  const displayH = (src.props.w * height) / width;
  const assetId = ensureAsset(editor, url, width, height);
  editor.createShape({
    type: "image",
    x: src.x + src.props.w + 24,
    y: src.y,
    props: { assetId, w: displayW, h: displayH },
  });
}

// ─── Placeholder shapes (for SSE progress feedback) ──────────────────

/**
 * Placeholders are TLDraw geo shapes (rectangle) with a dashed grey
 * outline + a text caption like "Generating · 8s\n<prompt>". The
 * dashed-grey style cues "this is in-progress" — distinct from any
 * real user-drawn shape so the user reads it as "waiting" without us
 * needing a custom shape type.
 *
 * Replacement strategy: when SSE done arrives, we capture the
 * placeholder's x/y, delete it, and create an image shape at the same
 * position. This means TLDraw's history records two ops (delete +
 * create) rather than a single "transform" — acceptable for now; a
 * custom TLDraw shape would unify them but adds ~200 LOC.
 *
 * We tag the placeholder via shape.meta.kind === "ai-placeholder" so
 * we can distinguish from user-drawn rectangles when scanning the
 * store (currently unused, but keeps the future "GC abandoned
 * placeholders" sweep cheap).
 */
function placePlaceholderOnCanvas(
  editor: Editor,
  prompt: string,
  width: number,
  height: number,
): string {
  const id = createShapeId();
  const bounds = editor.getViewportPageBounds();
  const displayW = width / 2;
  const displayH = height / 2;
  editor.createShape({
    id,
    type: "geo",
    x: bounds.midX - displayW / 2,
    y: bounds.midY - displayH / 2,
    meta: { kind: "ai-placeholder", prompt },
    props: {
      geo: "rectangle",
      w: displayW,
      h: displayH,
      dash: "dashed",
      color: "grey",
      fill: "none",
      text: placeholderCaption(0, "queued", prompt),
      // align/verticalAlign keep the caption centred even if text
      // wraps onto multiple lines.
      align: "middle",
      verticalAlign: "middle",
      size: "s",
    },
  });
  return id;
}

function updatePlaceholderProgress(
  editor: Editor,
  shapeId: string,
  elapsedS: number,
  statusLabel: string,
) {
  const id = shapeId as TLShapeId;
  const shape = editor.getShape(id);
  if (!shape || shape.type !== "geo") return;
  const prompt = (shape.meta?.prompt as string | undefined) ?? "";
  editor.updateShape({
    id,
    type: "geo",
    props: { text: placeholderCaption(elapsedS, statusLabel, prompt) },
  });
}

function replacePlaceholderWithImage(
  editor: Editor,
  shapeId: string,
  url: string,
  nativeWidth: number,
  nativeHeight: number,
) {
  const id = shapeId as TLShapeId;
  const placeholder = editor.getShape(id);
  if (!placeholder) {
    // Placeholder was deleted while task was in flight — drop the
    // image at viewport centre so the result isn't lost.
    placeImageOnCanvas(editor, url, nativeWidth, nativeHeight);
    return;
  }
  // Keep the placeholder's on-canvas dimensions so the replacement
  // doesn't visually pop in size. The asset record retains the
  // native resolution for high-DPI display.
  const x = placeholder.x;
  const y = placeholder.y;
  const w = (placeholder as { props: { w: number } }).props.w;
  const h = (placeholder as { props: { h: number } }).props.h;

  const assetId = ensureAsset(editor, url, nativeWidth, nativeHeight);
  editor.deleteShape(id);
  editor.createShape({
    type: "image",
    x,
    y,
    props: { assetId, w, h },
  });
}

function failPlaceholder(editor: Editor, shapeId: string, message: string) {
  const id = shapeId as TLShapeId;
  const shape = editor.getShape(id);
  if (!shape || shape.type !== "geo") return;
  const prompt = (shape.meta?.prompt as string | undefined) ?? "";
  editor.updateShape({
    id,
    type: "geo",
    props: {
      color: "red",
      text: `Failed: ${truncate(message, 80)}\n${truncate(prompt, 60)}`,
    },
  });
}

/**
 * focusShape selects a shape and zooms the viewport so it's centred
 * with a small margin. Used by the ChatCopilot history list to jump
 * to a previously-generated image. Safe no-op if the shape was
 * deleted — the user might prune the canvas without our knowing.
 */
function focusShape(editor: Editor, shapeId: string) {
  const id = shapeId as TLShapeId;
  const shape = editor.getShape(id);
  if (!shape) return;
  editor.select(id);
  const bounds = editor.getShapePageBounds(shape);
  if (!bounds) return;
  // Animate the zoom — easier to follow than an instant snap, esp.
  // when the user clicks rapidly between history entries.
  editor.zoomToBounds(bounds, { targetZoom: 1, animation: { duration: 220 } });
}

/** placeholderCaption — short status line + prompt preview. Two lines:
 *  the status banner ("Generating · 12s · running") and a truncated
 *  prompt so the user can tell which placeholder is which when they
 *  fire several. */
function placeholderCaption(elapsedS: number, status: string, prompt: string): string {
  const banner =
    elapsedS === 0
      ? `Generating · ${status}`
      : `Generating · ${elapsedS}s · ${status}`;
  return `${banner}\n${truncate(prompt, 60)}`;
}

function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max - 1) + "…";
}

// ─── Asset helpers ────────────────────────────────────────────────────

/** Idempotently create an asset for `url`. Returns the asset id (stable
 *  per URL via getHashForString), so calling twice with the same URL is
 *  a no-op on the second invocation. */
function ensureAsset(
  editor: Editor,
  url: string,
  width: number,
  height: number,
) {
  const assetId = AssetRecordType.createId(getHashForString(url));
  if (!editor.getAsset(assetId)) {
    editor.createAssets([
      {
        id: assetId,
        type: "image",
        typeName: "asset",
        meta: {},
        props: {
          name: "ai-image",
          src: url,
          w: width,
          h: height,
          mimeType: "image/png",
          isAnimated: false,
        },
      },
    ]);
  }
  return assetId;
}
