export type OtpauthFields = {
  secret: string;
  name?: string;
  issuer?: string;
  accountName?: string;
  digits?: number;
  period?: number;
  algorithm?: "SHA1" | "SHA256" | "SHA512";
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
  if (!/^totp$/i.test(url.host)) return null;

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

  const digits = Number(url.searchParams.get("digits"));
  const period = Number(url.searchParams.get("period"));
  const algorithm = (url.searchParams.get("algorithm") ?? "").toUpperCase();
  const name = issuer && accountName ? `${issuer} (${accountName})` : issuer || accountName;

  return {
    secret,
    ...(name ? { name } : {}),
    ...(issuer ? { issuer } : {}),
    ...(accountName ? { accountName } : {}),
    ...(Number.isInteger(digits) && digits >= 6 && digits <= 8 ? { digits } : {}),
    ...(Number.isInteger(period) && period >= 15 && period <= 120 ? { period } : {}),
    ...(algorithm === "SHA1" || algorithm === "SHA256" || algorithm === "SHA512"
      ? { algorithm: algorithm as "SHA1" | "SHA256" | "SHA512" }
      : {}),
  };
}
