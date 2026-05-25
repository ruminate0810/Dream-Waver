"use client";

import { useCallback, useRef } from "react";
import {
  AssetRecordType,
  getHashForString,
  Tldraw,
  type Editor,
} from "tldraw";
import "tldraw/tldraw.css";

// TldrawCanvas is the SSR-poison side of the design page — it imports
// `tldraw` and `tldraw/tldraw.css`, both of which touch `window` at
// module load. The parent page lazy-loads this via `next/dynamic` with
// `ssr: false` so a server render never executes this file.
//
// We expose a single imperative API to the parent: `onReady(placeImage)`
// fires once after TLDraw mounts; the parent stashes the `placeImage`
// callback and uses it whenever the AI panel finishes a generation.
// This keeps the parent decoupled from TLDraw's editor model — the
// panel only knows about (url, w, h), not about asset records or shape
// types.

export type TldrawCanvasProps = {
  /** Called once after the editor mounts. The parent uses the supplied
   *  function to drop AI-generated images onto the artboard. */
  onReady: (placeImage: (url: string, width: number, height: number) => void) => void;
};

export function TldrawCanvas({ onReady }: TldrawCanvasProps) {
  // editorRef lives across mounts so the placeImage closure stays
  // stable for the parent. Capturing `editor` in a closure passed to
  // onReady would mean we'd need to re-fire onReady every time TLDraw
  // remounted (which it shouldn't, but defensive is cheap).
  const editorRef = useRef<Editor | null>(null);

  const handleMount = useCallback(
    (editor: Editor) => {
      editorRef.current = editor;
      onReady((url, width, height) => {
        const ed = editorRef.current;
        if (!ed) return;
        placeImageOnCanvas(ed, url, width, height);
      });
    },
    [onReady],
  );

  return <Tldraw onMount={handleMount} />;
}

// placeImageOnCanvas drops a generated image at the centre of the
// current viewport at half its native resolution — that keeps a 1024px
// image comfortably visible in the typical 1440-wide canvas instead of
// dominating the entire frame. Asset and shape records are split per
// TLDraw's model: the asset carries the URL + native dimensions; the
// shape carries the on-canvas placement + display size.
function placeImageOnCanvas(
  editor: Editor,
  url: string,
  width: number,
  height: number,
) {
  // Asset IDs must be unique-per-URL. Hashing the URL gives us idempotent
  // IDs so re-generating the same prompt twice doesn't double-import the
  // asset; the second shape will share the first asset record.
  const assetId = AssetRecordType.createId(getHashForString(url));

  // Don't recreate the asset if it's already in the store.
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

  // Place at viewport centre. `editor.getViewportPageBounds()` returns
  // page-space coords; the shape's `x, y` are its top-left, so we
  // offset by half the display size to centre.
  const bounds = editor.getViewportPageBounds();
  const displayW = width / 2;
  const displayH = height / 2;
  const x = bounds.midX - displayW / 2;
  const y = bounds.midY - displayH / 2;

  editor.createShape({
    type: "image",
    x,
    y,
    props: {
      assetId,
      w: displayW,
      h: displayH,
    },
  });
}
