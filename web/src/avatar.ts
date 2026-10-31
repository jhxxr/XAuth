// Client-side preparation of profile pictures. The server accepts up to 1 MB
// of image bytes, but a phone photo is several megabytes, so the browser
// crops it to a square and shrinks it before it ever leaves the page.

/** Largest file the picker accepts before we even decode it. */
export const MAX_SOURCE_BYTES = 5 * 1024 * 1024;
/** Largest picture the server stores (mirrors store.MaxAvatarBytes). */
export const MAX_AVATAR_BYTES = 1024 * 1024;
/** Side length of the stored square picture. */
const TARGET_SIDE = 512;
/** Above this, a transparent PNG is re-encoded as JPEG to keep it small. */
const PNG_BUDGET = 192 * 1024;

export const AVATAR_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif"];
const AVATAR_EXTENSIONS = [".png", ".jpg", ".jpeg", ".webp", ".gif"];
export const AVATAR_ACCEPT = AVATAR_TYPES.join(",");

export function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(n % (1024 * 1024) === 0 ? 0 : 1)} MB`;
  return `${Math.max(1, Math.round(n / 1024))} KB`;
}

/** Human-readable reason a picked file is unacceptable, or "" when it is fine. */
export function checkAvatarFile(file: File): string {
  // Some systems report an empty MIME type for perfectly good pictures, so
  // fall back to the extension before refusing.
  const ext = (/\.[a-z0-9]+$/i.exec(file.name)?.[0] ?? "").toLowerCase();
  const known = file.type ? AVATAR_TYPES.includes(file.type) : AVATAR_EXTENSIONS.includes(ext);
  if (!known) return "请选择 PNG、JPEG、WebP 或 GIF 图片。";
  if (file.size > MAX_SOURCE_BYTES) return `图片不能超过 ${formatBytes(MAX_SOURCE_BYTES)}。`;
  return "";
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error("无法编码图片。"))), type, quality);
  });
}

function blobToDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error("无法读取图片。"));
    reader.readAsDataURL(blob);
  });
}

async function decode(file: File): Promise<ImageBitmap | HTMLImageElement> {
  if (typeof createImageBitmap === "function") {
    try {
      // `from-image` applies EXIF orientation, so portrait photos stay upright.
      return await createImageBitmap(file, { imageOrientation: "from-image" });
    } catch {
      // Fall through to the <img> path for formats createImageBitmap rejects.
    }
  }
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    return img;
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }
}

/**
 * Turns a picked file into a data: URL under the size limit. Throws an Error
 * whose message is safe to show when the file cannot be used.
 */
export async function prepareAvatar(file: File): Promise<string> {
  const problem = checkAvatarFile(file);
  if (problem) throw new Error(problem);

  const image = await decode(file);
  const width = "naturalWidth" in image ? image.naturalWidth : image.width;
  const height = "naturalHeight" in image ? image.naturalHeight : image.height;
  if (!width || !height) throw new Error("这张图片无法打开，请换一张。");

  // Centre crop to a square, then scale down to the target side.
  const side = Math.min(width, height);
  const size = Math.min(TARGET_SIDE, side);
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("这个浏览器不支持处理图片。");
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(image, (width - side) / 2, (height - side) / 2, side, side, 0, 0, size, size);
  if ("close" in image) image.close();

  let blob = await canvasToBlob(canvas, "image/png");
  if (blob.size > PNG_BUDGET) {
    // Photos are far smaller as JPEG. Composited on white so transparent
    // pixels do not turn black.
    ctx.globalCompositeOperation = "destination-over";
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, size, size);
    blob = await canvasToBlob(canvas, "image/jpeg", 0.86);
  }
  if (blob.size > MAX_AVATAR_BYTES) {
    throw new Error(`压缩后仍然超过 ${formatBytes(MAX_AVATAR_BYTES)}，请换一张更小的图片。`);
  }
  return blobToDataURL(blob);
}
