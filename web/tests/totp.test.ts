import assert from "node:assert/strict";
import test from "node:test";
import { codeDeadline, codeSecondsLeft, parseOtpauth } from "../src/totp.ts";

test("import keeps the URI's algorithm, digits and period", () => {
  const entry = parseOtpauth("otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP&digits=8&period=10&algorithm=SHA256");
  assert.equal(entry?.digits, 8);
  assert.equal(entry?.period, 10);
  assert.equal(entry?.algorithm, "SHA256");
  assert.equal(entry?.accountName, "alice");
});

test("network delay consumes validity even when the browser clock differs", () => {
  const startedAt = Date.parse("2026-11-21T12:34:00Z");
  // The server clock is two hours ahead, and this response took 1500 ms.
  const deadline = codeDeadline({ generatedAt: startedAt + 7200000, expiresAt: startedAt + 7202000 }, startedAt);
  assert.equal(codeSecondsLeft(deadline, startedAt + 1500), 1);
  assert.equal(codeSecondsLeft(deadline, startedAt + 2000), 0);
  assert.equal(codeSecondsLeft(deadline, startedAt + 30000), 0);
});

test("refreshing an identical code still gives it the new validity window", () => {
  assert.equal(codeDeadline({ remaining: 20 }, 1000), 21000);
  assert.equal(codeDeadline({ remaining: 20 }, 2000), 22000);
  assert.equal(codeDeadline({}, 1000), 1000);
});

test("an ordinary URI supplies all three defaults when replacing another URI", () => {
  const entry = parseOtpauth("otpauth://totp/Example:alice?secret=JBSWY3DPEHPK3PXP");
  assert.equal(entry?.digits, 6);
  assert.equal(entry?.period, 30);
  assert.equal(entry?.algorithm, "SHA1");
});

test("invalid parameters are never silently replaced with defaults", () => {
  for (const query of ["digits=5", "digits=0", "digits=oops", "period=0", "period=30.5", "period=86401", "algorithm=MD5", "encoder=steam", "digits=6&digits=8"]) {
    assert.equal(parseOtpauth(`otpauth://totp/test?secret=JBSWY3DPEHPK3PXP&${query}`), null, query);
  }
});
