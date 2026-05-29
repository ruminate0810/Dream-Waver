"use client";

import { useCallback, useEffect, useRef } from "react";
import {
  AssetRecordType,
  createShapeId,
  exportToBlob,
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
 *  empty / multi / a non-image shape.
 *
 *  Carries TWO coordinate spaces:
 *   - page* : TLDraw page-space (independent of pan/zoom)
 *   - screen*: CSS pixels relative to the canvas wrapper, ready to
 *              drop into an absolute-positioned toolbar's left/top.
 *  Screen coords update on every pan / zoom / drag so the toolbar
 *  follows the shape without re-anchoring code on the parent. */
export type SelectedImageInfo = {
  shapeId: string;
  /** The on-canvas image URL (i.e. the asset's `src`). */
  url: string;
  /** Native dimensions from the asset record (NOT the display size). */
  width: number;
  height: number;
  /** Page-space top-left + display size. Stable across viewport changes. */
  pageX: number;
  pageY: number;
  displayW: number;
  displayH: number;
  /** Screen-space top-left + size (CSS pixels, canvas-wrapper origin).
   *  Reflects current zoom — a 1024-page-wide image at 50% zoom has
   *  screenW = 512. */
  screenX: number;
  screenY: number;
  screenW: number;
  screenH: number;
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
  /** Drop a TLDraw video shape next to the currently-selected image.
   *  Used by Seedance i2v — the resulting MP4 sits beside its source
   *  image so the before/after pair stays visually grouped. Falls back
   *  to viewport centre when no image is selected. */
  placeVideoNextToSelection: (videoURL: string) => void;
  /** Place a user-uploaded image on the canvas (file picker / drag).
   *  Tagged as "user-upload" (not "ai-image") so the SelectionToolbar
   *  doesn't appear for it — uploads from a file have data:URLs that
   *  the upstream gateway can't fetch, so the AI ops would 502.
   *  Dimensions are caller-supplied (read off the natural image). */
  placeUploadedImage: (
    dataURL: string,
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
  /** Undo the most recent canvas mutation (TLDraw's own history stack).
   *  No-op when the stack is empty. */
  undo: () => void;
  /** Redo the most recently undone canvas mutation. No-op when the
   *  redo stack is empty. */
  redo: () => void;
  /** Export the whole current page (or current selection if any) as
   *  a Blob in the requested format. Uses TLDraw's `exportToBlob`
   *  helper. Returns null when there's nothing to export (empty
   *  canvas) so the caller can show a friendly notice. */
  exportCanvas: (format: "png" | "svg") => Promise<Blob | null>;
  /** Check whether the canvas has any user-drawn / placed shapes.
   *  Used by the header's "Export" button to grey out when empty
   *  and by the sketch-to-image button to know when there's a
   *  region to capture. */
  hasShapes: () => boolean;
  /** Capture a region of the canvas as a PNG blob for sketch-to-image.
   *  Behaviour:
   *    - If user has shapes selected, exports just those (lets the
   *      user crop precisely by selecting a frame / region).
   *    - Otherwise, exports all NON-image shapes on the page — the
   *      "everything I drew" path. Existing AI images don't pollute
   *      the sketch ref.
   *  Returns null when there's nothing to capture. */
  captureSketchRegion: () => Promise<Blob | null>;
  /** Check whether the canvas has any drawable (non-image) shapes
   *  the sketch button could use. Drives the disabled state of the
   *  pencil button. */
  hasSketchableShapes: () => boolean;
};

/** Reactive flags surfaced to the header's Undo / Redo buttons so they
 *  can show enabled/disabled state. Updated via the `onHistoryFlags`
 *  callback on every TLDraw store mutation. */
export type CanvasHistoryFlags = {
  canUndo: boolean;
  canRedo: boolean;
};

export type TldrawCanvasProps = {
  onReady: (controller: CanvasController) => void;
  onSelectionChange?: (info: SelectedImageInfo | null) => void;
  /** Fires on every TLDraw store mutation so the parent's header can
   *  reflect can-undo / can-redo state on its buttons. Coalesced by
   *  React's setState — calling this with the same {canUndo, canRedo}
   *  is cheap. */
  onHistoryFlagsChange?: (flags: CanvasHistoryFlags) => void;
};

export function TldrawCanvas({
  onReady,
  onSelectionChange,
  onHistoryFlagsChange,
}: TldrawCanvasProps) {
  const editorRef = useRef<Editor | null>(null);
  // selectionCallback held in a ref so the store-listen subscription
  // (which captures it at install time) sees the latest version even
  // when the parent passes a new function on re-render.
  const selectionCallbackRef = useRef(onSelectionChange);
  useEffect(() => {
    selectionCallbackRef.current = onSelectionChange;
  }, [onSelectionChange]);
  const historyCallbackRef = useRef(onHistoryFlagsChange);
  useEffect(() => {
    historyCallbackRef.current = onHistoryFlagsChange;
  }, [onHistoryFlagsChange]);

  const handleMount = useCallback(
    (editor: Editor) => {
      editorRef.current = editor;

      const controller: CanvasController = {
        placeImage: (url, w, h) => placeImageOnCanvas(editor, url, w, h),
        placeVariants: (variants) => placeVariantsOnCanvas(editor, variants),
        placeNextToSelection: (url, w, h) =>
          placeImageNextToSelection(editor, url, w, h),
        placeVideoNextToSelection: (videoURL) =>
          placeVideoNextToSelection(editor, videoURL),
        placeUploadedImage: (dataURL, w, h) =>
          placeUploadedImageOnCanvas(editor, dataURL, w, h),
        placePlaceholder: (prompt, w, h) =>
          placePlaceholderOnCanvas(editor, prompt, w, h),
        updatePlaceholderProgress: (id, elapsed, label) =>
          updatePlaceholderProgress(editor, id, elapsed, label),
        replacePlaceholderWithImage: (id, url, w, h) =>
          replacePlaceholderWithImage(editor, id, url, w, h),
        failPlaceholder: (id, msg) => failPlaceholder(editor, id, msg),
        focusShape: (id) => focusShape(editor, id),
        undo: () => editor.undo(),
        redo: () => editor.redo(),
        exportCanvas: (format) => exportCanvasBlob(editor, format),
        hasShapes: () => editor.getCurrentPageShapes().length > 0,
        captureSketchRegion: () => captureSketchRegionBlob(editor),
        hasSketchableShapes: () =>
          editor.getCurrentPageShapes().some((s) => s.type !== "image" && s.type !== "video"),
      };
      onReady(controller);

      // Selection bridge — fire onSelectionChange whenever the user's
      // selection lands on / leaves an image shape. `editor.store.listen`
      // delivers EVERY store mutation, including drags and resizes, so
      // we always send the freshest bounds for the floating toolbar.
      const disposeSelection = editor.store.listen(
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

      // History flags bridge — separate listener with NO source filter
      // so programmatic mutations (placeImage / placeholder swap) push
      // canUndo too. The header buttons would otherwise stay disabled
      // after we drop a generation on the canvas.
      const pushHistoryFlags = () => {
        const hcb = historyCallbackRef.current;
        if (!hcb) return;
        hcb({ canUndo: editor.getCanUndo(), canRedo: editor.getCanRedo() });
      };
      const disposeHistory = editor.store.listen(pushHistoryFlags, {
        scope: "all",
      });
      // Fire once on mount so the header reflects the persisted state.
      pushHistoryFlags();

      return () => {
        disposeSelection();
        disposeHistory();
      };
    },
    [onReady],
  );

  return <Tldraw onMount={handleMount} components={TLDRAW_COMPONENTS} />;
}

// TLDraw chrome overrides. We hide the style/menu panels that fight
// our right-side chat for screen real estate — the user picks
// generation settings in the chat, not in TLDraw. The canvas itself
// keeps its left page panel + bottom toolbar so the drawing tools
// (select / draw / arrow / text) stay accessible.
//
// Setting a component to `null` hides it. The list of components is
// in TLDraw's TLEditorComponents — see `tldraw/dist-cjs/index.d.ts`.
const TLDRAW_COMPONENTS = {
  // Top-right style panel (colour swatches + size S/M/L/XL +
  // alignment grid) — clashed with the chat panel's header. Users
  // pick model/aspect/style in the chat, so this is redundant.
  StylePanel: null,
  // Hide the share zone and help button — minimal UI for the canvas.
  HelpMenu: null,
  SharePanel: null,
} as const;

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

  // Convert page-space corners to screen-space (CSS px from the
  // viewport top-left). The toolbar layer is positioned RELATIVE to
  // the TLDraw container — same coordinate system — so we just hand
  // these back and the caller's `style.left/top` work directly.
  // pageToScreen() is exposed on Editor; both corner conversions live
  // inside one call site so a zoom change between them is impossible.
  const topLeft = editor.pageToScreen({ x: img.x, y: img.y });
  const bottomRight = editor.pageToScreen({
    x: img.x + img.props.w,
    y: img.y + img.props.h,
  });

  return {
    shapeId: img.id,
    url: src,
    width: asset.props.w,
    height: asset.props.h,
    pageX: img.x,
    pageY: img.y,
    displayW: img.props.w,
    displayH: img.props.h,
    screenX: topLeft.x,
    screenY: topLeft.y,
    screenW: bottomRight.x - topLeft.x,
    screenH: bottomRight.y - topLeft.y,
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
 * placeUploadedImageOnCanvas drops a user-uploaded image (data URL) at
 * viewport centre. Same display sizing as placeImageOnCanvas but tagged
 * "user-upload" on the asset so readSelectedImage skips it — the
 * SelectionToolbar's AI ops would 502 on a data: URL anyway (the
 * upstream gateway fetches the URL itself; data URLs aren't fetchable).
 *
 * The user can still drag, resize, delete the upload via TLDraw's
 * built-in tools — they just can't run Reimagine / Variants / Animate
 * until we wire a hosted upload endpoint.
 */
function placeUploadedImageOnCanvas(
  editor: Editor,
  dataURL: string,
  width: number,
  height: number,
) {
  const assetId = AssetRecordType.createId(getHashForString("upload::" + dataURL.slice(0, 64)));
  if (!editor.getAsset(assetId)) {
    editor.createAssets([
      {
        id: assetId,
        type: "image",
        typeName: "asset",
        meta: {},
        props: {
          // Tagged "ai-image" (same as generated assets) so the
          // SelectionToolbar shows for uploads — sidecar now decodes
          // data: URLs into proper Gemini inlineData with mimeType,
          // so AI ops like Reimagine / Variants / Animate actually
          // work on uploaded images. (Animate via Seedance still
          // needs a public URL since its API takes image_url, not
          // inlineData — Reimagine/Variants/Edit are the working set.)
          name: "ai-image",
          src: dataURL,
          w: width,
          h: height,
          mimeType: dataURL.startsWith("data:image/png") ? "image/png" : "image/jpeg",
          isAnimated: false,
        },
      },
    ]);
  }
  const bounds = editor.getViewportPageBounds();
  // Cap display at 512 on the larger axis so a giant 4K upload doesn't
  // explode the canvas. Aspect ratio preserved.
  const maxAxis = 512;
  const scale = Math.min(1, maxAxis / Math.max(width, height));
  const displayW = width * scale;
  const displayH = height * scale;
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
 * exportCanvasBlob renders the current page (or the user's selection
 * when they have one) to a Blob in the requested format. Uses
 * TLDraw's `exportToBlob` helper which under the hood:
 *   - PNG: rasterises shapes onto a canvas at 2× device pixel ratio
 *   - SVG: composes a real <svg> document with all shapes vectorised
 *
 * Returns null when the canvas is empty so the caller can show a
 * friendly "nothing to export" notice instead of saving a blank file.
 *
 * Selection-first behaviour matches what most design tools do — if
 * the user has highlighted a subset, that's what they probably want
 * to export. Empty selection falls back to the whole page.
 */
async function exportCanvasBlob(
  editor: Editor,
  format: "png" | "svg",
): Promise<Blob | null> {
  const selected = editor.getSelectedShapes();
  const targets =
    selected.length > 0 ? selected : editor.getCurrentPageShapes();
  if (targets.length === 0) return null;
  const ids = targets.map((s) => s.id);
  return exportToBlob({ editor, ids, format });
}

/**
 * captureSketchRegionBlob renders the user's drawn strokes (TLDraw
 * draw / highlight / shape shapes — anything NOT an image or video)
 * to a transparent-background PNG. The result is fed back into
 * NanoBanana as a reference, so the model is conditioned by the
 * user's freehand layout without other AI images polluting the ref.
 *
 * Honours an explicit selection — when the user has shapes selected
 * (could include images), those are exported instead. Lets advanced
 * users compose "sketch + existing image" refs deliberately.
 *
 * Returns null when there are no drawable shapes to capture so the
 * pencil button's call site can show a "nothing to send" notice.
 */
async function captureSketchRegionBlob(editor: Editor): Promise<Blob | null> {
  const selected = editor.getSelectedShapes();
  let targets;
  if (selected.length > 0) {
    targets = selected;
  } else {
    // Drawables = everything except image / video shapes. TLDraw's
    // draw / highlight / line / arrow / geo / text / note all qualify.
    targets = editor
      .getCurrentPageShapes()
      .filter((s) => s.type !== "image" && s.type !== "video");
  }
  if (targets.length === 0) return null;
  const ids = targets.map((s) => s.id);
  // background: false → transparent PNG, so the sketch isn't on a
  // misleading white plate when Gemini interprets it as ref context.
  return exportToBlob({
    editor,
    ids,
    format: "png",
    opts: { background: false },
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

/** Mirror of ensureAsset for video. TLDraw uses a separate asset type
 *  for video — the URL hash collision space is shared with images, so
 *  if a video URL ever coincided with an existing image URL we'd hit
 *  a type mismatch; the prefix below keeps that from happening. */
function ensureVideoAsset(
  editor: Editor,
  url: string,
  width: number,
  height: number,
) {
  // Prefix to keep the asset id distinct from any same-URL image hash.
  const assetId = AssetRecordType.createId(getHashForString("video::" + url));
  if (!editor.getAsset(assetId)) {
    editor.createAssets([
      {
        id: assetId,
        type: "video",
        typeName: "asset",
        meta: {},
        props: {
          name: "ai-video",
          src: url,
          w: width,
          h: height,
          mimeType: "video/mp4",
          isAnimated: true,
        },
      },
    ]);
  }
  return assetId;
}

/** Place a TLDraw video shape next to the currently-selected image —
 *  used by Seedance i2v so the result sits visually grouped with its
 *  source. Native dimensions come from the source image (Seedance
 *  preserves the input ratio in practice); the canvas reads the real
 *  dimensions off the loaded <video> when TLDraw renders it.
 *
 *  Falls back to viewport centre with a 512×512 placeholder size when
 *  no image is selected — the source-relative position is the common
 *  case but not strictly required. */
function placeVideoNextToSelection(editor: Editor, url: string) {
  const selected = editor.getSelectedShapes();
  let x: number, y: number, displayW: number, displayH: number;
  let nativeW: number, nativeH: number;
  if (selected.length === 1 && selected[0].type === "image") {
    const src = selected[0] as TLImageShape;
    displayW = src.props.w;
    displayH = src.props.h;
    nativeW = src.props.w * 2; // matches the ensureAsset half-resolution convention
    nativeH = src.props.h * 2;
    x = src.x + src.props.w + 24;
    y = src.y;
  } else {
    const bounds = editor.getViewportPageBounds();
    nativeW = 1024;
    nativeH = 1024;
    displayW = 512;
    displayH = 512;
    x = bounds.midX - displayW / 2;
    y = bounds.midY - displayH / 2;
  }
  const assetId = ensureVideoAsset(editor, url, nativeW, nativeH);
  editor.createShape({
    type: "video",
    x,
    y,
    props: { assetId, w: displayW, h: displayH },
  });
}
