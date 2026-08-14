import { describe, expect, it } from "vitest";
import type { ApiError } from "../api/types";
import {
  MAX_RESIDENCY_CLAIM_LENGTH,
  describeResidencyClaim,
  residencyClaimErrorMessage,
  residencyClaimOf,
  validateResidencyClaim,
} from "./residency";

/**
 * The claim is an attestation, not an address the town holds. These pin the
 * wording that keeps it readable as one, and the bound that keeps the form from
 * sending what the server will refuse.
 */

describe("residencyClaimOf", () => {
  it("reads the claim off a profile that carries one", () => {
    expect(residencyClaimOf({ residency_claim: "By the old mill" })).toBe("By the old mill");
  });

  // The self view may not carry the field at all, depending on the server this
  // build is talking to. An empty box is the right answer either way.
  it("reads as empty when the profile does not carry the field", () => {
    expect(residencyClaimOf({})).toBe("");
  });

  it("reads as empty for no profile at all", () => {
    expect(residencyClaimOf(null)).toBe("");
    expect(residencyClaimOf(undefined)).toBe("");
  });

  it("trims, so a whitespace-only claim is no claim", () => {
    expect(residencyClaimOf({ residency_claim: "   " })).toBe("");
    expect(residencyClaimOf({ residency_claim: "  Mill Lane  " })).toBe("Mill Lane");
  });
});

describe("validateResidencyClaim", () => {
  // Clearing what you said is a thing a member is entitled to do, and the empty
  // string is how the endpoint documents doing it.
  it("accepts an empty claim, which clears it", () => {
    expect(validateResidencyClaim("")).toEqual({ valid: true });
    expect(validateResidencyClaim("   ")).toEqual({ valid: true });
  });

  it("accepts a claim at the limit", () => {
    expect(validateResidencyClaim("a".repeat(MAX_RESIDENCY_CLAIM_LENGTH))).toEqual({ valid: true });
  });

  it("refuses a claim one character over, and says by how much", () => {
    const result = validateResidencyClaim("a".repeat(MAX_RESIDENCY_CLAIM_LENGTH + 1));
    expect(result.valid).toBe(false);
    expect(result.error).toContain(String(MAX_RESIDENCY_CLAIM_LENGTH + 1));
  });

  // The bound is written in characters on the server, so an emoji is one of
  // them — measuring with JS .length would count it twice and refuse a claim
  // the server accepts.
  it("counts characters rather than UTF-16 units", () => {
    expect(validateResidencyClaim("🏠".repeat(MAX_RESIDENCY_CLAIM_LENGTH))).toEqual({ valid: true });
  });

  it("measures the trimmed claim, not the whitespace around it", () => {
    const padded = `  ${"a".repeat(MAX_RESIDENCY_CLAIM_LENGTH)}  `;
    expect(validateResidencyClaim(padded)).toEqual({ valid: true });
  });
});

describe("describeResidencyClaim", () => {
  // "Says they're" is load-bearing: the council is reading a claim, and a bare
  // string under a name reads as a fact the town checked.
  it("attributes the claim to the person making it", () => {
    expect(describeResidencyClaim("Mill Lane")).toBe("Says they're at or near Mill Lane");
  });

  it("has nothing to say when no claim was given", () => {
    expect(describeResidencyClaim("")).toBeNull();
    expect(describeResidencyClaim("   ")).toBeNull();
    expect(describeResidencyClaim(null)).toBeNull();
    expect(describeResidencyClaim(undefined)).toBeNull();
  });
});

describe("residencyClaimErrorMessage", () => {
  const err = (status: number, error = "boom"): ApiError => ({ status, error });

  it("passes on what the server said about a rejected claim", () => {
    expect(residencyClaimErrorMessage(err(400, "validation error: claim is too long"))).toBe(
      "Claim is too long.",
    );
  });

  it("names the unopened verification email rather than blaming the claim", () => {
    const message = residencyClaimErrorMessage(err(403, "email not verified"));
    expect(message).toMatch(/verif/i);
  });

  it("falls back to something actionable for an unexplained failure", () => {
    expect(residencyClaimErrorMessage(err(500))).toMatch(/try again/i);
    expect(residencyClaimErrorMessage(null)).toMatch(/try again/i);
  });
});
