import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { PRODUCT_NAME, documentTitle, useDocumentTitle } from "./useDocumentTitle";

afterEach(() => {
  document.title = "";
});

describe("documentTitle", () => {
  it("names the tab for the town, with the product behind it", () => {
    expect(documentTitle("Millbrook")).toBe("Millbrook · The Bell");
  });

  // Before the config request lands there is no town name, and the tab still
  // has to say something truthful.
  it.each<[string | null | undefined, string]>([
    [undefined, "no name yet"],
    [null, "an absent name"],
    ["", "an empty name"],
    ["   ", "a name that is only whitespace"],
  ])("falls back to the product name for %s (%s)", (input) => {
    expect(documentTitle(input)).toBe(PRODUCT_NAME);
  });

  // A town that has not renamed itself carries the product name as its own.
  it("does not say the product name twice", () => {
    expect(documentTitle("The Bell")).toBe("The Bell");
  });

  it("trims a pasted name rather than padding the tab", () => {
    expect(documentTitle("  Millbrook  ")).toBe("Millbrook · The Bell");
  });
});

describe("useDocumentTitle", () => {
  it("sets the tab from the town name", () => {
    renderHook(() => useDocumentTitle("Millbrook"));
    expect(document.title).toBe("Millbrook · The Bell");
  });

  // The name arrives from town config after first paint, so the hook has to
  // follow the value rather than run once.
  it("follows a name that arrives late", () => {
    const { rerender } = renderHook(({ name }) => useDocumentTitle(name), {
      initialProps: { name: undefined as string | undefined },
    });
    expect(document.title).toBe(PRODUCT_NAME);

    rerender({ name: "Millbrook" });
    expect(document.title).toBe("Millbrook · The Bell");
  });

  // A council member can rename the town from Town Hall without a reload.
  it("follows a rename", () => {
    const { rerender } = renderHook(({ name }) => useDocumentTitle(name), {
      initialProps: { name: "Millbrook" },
    });
    rerender({ name: "Millbrook Falls" });
    expect(document.title).toBe("Millbrook Falls · The Bell");
  });
});
