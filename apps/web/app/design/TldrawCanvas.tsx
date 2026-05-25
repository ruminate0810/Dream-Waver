"use client";

import { useCallback, useEffect, useRef } from "react";
import {
  AssetRecordType,
  getHashForString,
  Tldraw,
  type Editor,
  type TLImageShape,
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
