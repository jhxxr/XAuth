export type OtpauthFields = {
  secret: string;
  name?: string;
  issuer?: string;
  accountName?: string;
  digits: number;
  period: number;
  algorithm: "SHA1" | "SHA256" | "SHA512";
};

/**
 * Read the form fields out of an otpauth://totp URI.
 * Returns null for anything else (a bare Base32 secret, text, a bad link).
 * The server still validates the input; this only pre-fills the form so a
 * scanned QR code keeps its digits/period/algorithm instead of the defaults.
 */
export function parseOtpauth(raw: string): OtpauthFields | null {
  const value = raw.trim();
  if (!/^otpauth:\/\//i.test(value)) return null;

  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return null;
  }
  if (!/^totp$/i.test(url.host) || url.username || url.password || url.hash) return null;
  for (const key of ["secret", "issuer", "digits", "period", "algorithm", "encoder"]) {
    if (url.searchParams.getAll(key).length > 1) return null;
  }
  if (url.searchParams.get("encoder")) return null;

  const secret = url.searchParams.get("secret")?.trim();
  if (!secret) return null;

  let label = url.pathname.replace(/^\/+/, "");
  try {
    label = decodeURIComponent(label);
  } catch {
    // Keep the raw label when it contains a stray percent sign.
  }
  const separator = label.indexOf(":");
  const labelIssuer = separator >= 0 ? label.slice(0, separator).trim() : "";
  const accountName = (separator >= 0 ? label.slice(separator + 1) : label).trim();
  const issuer = (url.searchParams.get("issuer") ?? "").trim() || labelIssuer;

  const digitsText = url.searchParams.get("digits") ?? "6";
  const periodText = url.searchParams.get("period") ?? "30";
  if (!/^\d+$/.test(digitsText) || !/^\d+$/.test(periodText)) return null;
  const digits = Number(digitsText);
  const period = Number(periodText);
  const algorithm = (url.searchParams.get("algorithm")?.trim() || "SHA1").toUpperCase();
  if (!Number.isInteger(digits) || digits < 6 || digits > 8) return null;
  if (!Number.isInteger(period) || period < 1 || period > 86400) return null;
  if (algorithm !== "SHA1" && algorithm !== "SHA256" && algorithm !== "SHA512") return null;
  const name = issuer && accountName ? `${issuer} (${accountName})` : issuer || accountName;

  return {
    secret,
    ...(name ? { name } : {}),
    ...(issuer ? { issuer } : {}),
    ...(accountName ? { accountName } : {}),
    digits,
    period,
    algorithm,
  };
}

/** Map the server's validity window to this browser's clock at request start.
 * Using request start is conservative: network delay cannot extend an old code.
 * The wall-clock deadline also remains correct after a suspended tab wakes up.
 */
export function codeDeadline(entry: { generatedAt?: number; expiresAt?: number; remaining?: number }, requestedAt: number): number {
  const lifetime = entry.generatedAt !== undefined && entry.expiresAt !== undefined
    ? entry.expiresAt - entry.generatedAt
    : (entry.remaining ?? 0) * 1000;
  return requestedAt + Math.max(0, lifetime);
}

export function codeSecondsLeft(deadline: number, now: number): number {
  return Math.max(0, Math.ceil((deadline - now) / 1000));
}
