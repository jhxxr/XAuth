import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import { Button, Notice } from "./ui";
import { Icon } from "./Icon";

type QrScannerModule = typeof import("qr-scanner");
type QrScannerCtor = QrScannerModule["default"];
type QrScannerInstance = InstanceType<QrScannerCtor>;

// qr-scanner is a ~50 kB chunk; only fetch it when the user actually imports.
let modulePromise: Promise<QrScannerModule> | null = null;
function loadQrScanner(): Promise<QrScannerCtor> {
  modulePromise ??= import("qr-scanner");
  return modulePromise.then((module) => module.default);
}

function decodeError(cause: unknown, fallback: string): string {
  if (cause instanceof DOMException) {
    if (cause.name === "NotAllowedError") return "浏览器拒绝了权限。请允许访问摄像头或剪贴板，或改用上传图片。";
    if (cause.name === "NotFoundError") return "没有找到可用的摄像头。";
    if (cause.name === "NotReadableError") return "摄像头被其他程序占用，或系统不允许访问。";
  }
  const message = cause instanceof Error ? cause.message : typeof cause === "string" ? cause : "";
  if (!message) return fallback;
  if (/no qr code found/i.test(message)) return "没有在图片中识别到二维码，换一张更清晰的截图试试。";
  if (/no camera found/i.test(message)) return "没有找到可用的摄像头。";
  return message;
}

function isTotpPayload(value: string): boolean {
  if (/^otpauth:\/\//i.test(value)) return true;
  return /^[A-Z2-7=\s-]+$/i.test(value);
}

type Props = {
  /** Called with the decoded raw value (otpauth URI or Base32 secret). */
  onDecoded: (raw: string) => void;
  disabled?: boolean;
};

export function QrImport({ onDecoded, disabled }: Props) {
  const [busy, setBusy] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const scannerRef = useRef<QrScannerInstance | null>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);

  const stopCamera = useCallback(() => {
    const scanner = scannerRef.current;
    scannerRef.current = null;
    if (scanner) {
      try {
        scanner.stop();
        scanner.destroy();
      } catch {
        // The stream may already be gone (tab closed, device unplugged).
      }
    }
    setScanning(false);
  }, []);

  useEffect(() => stopCamera, [stopCamera]);

  const accept = useCallback(
    (raw: string) => {
      const value = raw.trim();
      if (!value) throw new Error("二维码内容为空。");
      if (/^otpauth-migration:\/\//i.test(value)) {
        throw new Error("暂不支持 Google Authenticator 的迁移二维码，请改用单个验证器的二维码。");
      }
      if (!isTotpPayload(value)) {
        throw new Error("这个二维码不是 TOTP 验证器二维码。");
      }
      onDecoded(value);
      setError("");
      setDone(true);
    },
    [onDecoded],
  );

  const decodeFile = useCallback(
    async (file: File) => {
      const QrScanner = await loadQrScanner();
      const result = await QrScanner.scanImage(file, { returnDetailedScanResult: true });
      accept(result.data);
    },
    [accept],
  );

  const handleFile = useCallback(
    async (file: File) => {
      setBusy(true);
      setError("");
      setDone(false);
      try {
        await decodeFile(file);
      } catch (cause) {
        setError(decodeError(cause, "无法读取这张图片。"));
      } finally {
        setBusy(false);
      }
    },
    [decodeFile],
  );

  const pickFile = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (file) void handleFile(file);
  };

  const pasteFromClipboard = async () => {
    setBusy(true);
    setError("");
    setDone(false);
    try {
      const items = navigator.clipboard?.read ? await navigator.clipboard.read() : [];
      for (const item of items) {
        const type = item.types.find((candidate) => candidate.startsWith("image/"));
        if (!type) continue;
        const blob = await item.getType(type);
        await decodeFile(new File([blob], "clipboard", { type }));
        return;
      }
      const text = navigator.clipboard?.readText ? (await navigator.clipboard.readText()).trim() : "";
      if (!text) throw new Error("剪贴板里没有二维码图片或 otpauth 链接。");
      accept(text);
    } catch (cause) {
      setError(decodeError(cause, "读取剪贴板失败。可以复制截图后，在此表单内按 Ctrl+V 粘贴。"));
    } finally {
      setBusy(false);
    }
  };

  const startCamera = async () => {
    const video = videoRef.current;
    if (!video || scannerRef.current) return;
    setBusy(true);
    setError("");
    setDone(false);
    try {
      const QrScanner = await loadQrScanner();
      if (!(await QrScanner.hasCamera())) throw new Error("未检测到可用摄像头。");
      const scanner = new QrScanner(
        video,
        (result) => {
          try {
            accept(result.data);
            stopCamera();
          } catch (cause) {
            setError(decodeError(cause, "无法识别这个二维码。"));
          }
        },
        {
          preferredCamera: "environment",
          maxScansPerSecond: 10,
          returnDetailedScanResult: true,
        },
      );
      scanner.setInversionMode("both");
      scannerRef.current = scanner;
      await scanner.start();
      setScanning(true);
    } catch (cause) {
      stopCamera();
      setError(decodeError(cause, "无法打开摄像头。"));
    } finally {
      setBusy(false);
    }
  };

  // Pasting a screenshot anywhere in the form imports it (text paste still
  // reaches the secret textarea untouched).
  useEffect(() => {
    const onPaste = (event: ClipboardEvent) => {
      const image = Array.from(event.clipboardData?.files ?? []).find((file) => file.type.startsWith("image/"));
      if (!image) return;
      event.preventDefault();
      void handleFile(image);
    };
    window.addEventListener("paste", onPaste);
    return () => window.removeEventListener("paste", onPaste);
  }, [handleFile]);

  return (
    <section className="rounded-xl border border-dashed border-line bg-desk/25 p-4">
      <div className="flex items-start gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg border border-line bg-paper text-ink-soft">
          <Icon name="qr" className="size-4.5" />
        </span>
        <div className="min-w-0">
          <h3 className="text-[0.8125rem] font-medium text-ink">扫码 / 粘贴导入</h3>
          <p className="mt-0.5 text-[0.8125rem] leading-5 text-ink-soft">
            支持验证器二维码截图、剪贴板图片，或直接粘贴 otpauth 链接。二维码只在当前浏览器解析，不会上传。
          </p>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button type="button" variant="secondary" size="sm" disabled={disabled || busy || scanning} onClick={() => fileRef.current?.click()}>
          <Icon name="image" className="size-4" />
          上传图片
        </Button>
        <Button type="button" variant="secondary" size="sm" disabled={disabled || busy || scanning} onClick={() => void pasteFromClipboard()}>
          <Icon name="clipboard" className="size-4" />
          读取剪贴板
        </Button>
        <Button
          type="button"
          variant={scanning ? "ghost" : "secondary"}
          size="sm"
          disabled={disabled || busy}
          onClick={() => (scanning ? stopCamera() : void startCamera())}
        >
          <Icon name={scanning ? "x" : "camera"} className="size-4" />
          {scanning ? "停止扫码" : "摄像头扫码"}
        </Button>
        {busy && <span className="text-[0.8125rem] text-ink-soft">正在识别…</span>}
      </div>

      <input ref={fileRef} type="file" accept="image/*" className="sr-only" onChange={pickFile} aria-label="选择二维码图片" />

      <div className={scanning ? "mt-3 overflow-hidden rounded-xl border border-line bg-black" : "hidden"}>
        <video ref={videoRef} className="aspect-video w-full bg-black object-contain" muted playsInline aria-label="摄像头预览" />
      </div>

      {error && (
        <div className="mt-3">
          <Notice>{error}</Notice>
        </div>
      )}
      {done && !error && (
        <p role="status" className="mt-3 flex items-center gap-1.5 text-[0.8125rem] text-ink-soft">
          <Icon name="check" className="size-4 text-ink" />
          已识别，参数已填入下方表单。
        </p>
      )}
    </section>
  );
}
