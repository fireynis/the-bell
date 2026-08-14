import { useEffect } from "react";

/** What the tab says before the town's own name has arrived. */
export const PRODUCT_NAME = "The Bell";

/**
 * documentTitle names the tab for a town.
 *
 * The product name is always the tail, because a bare "Millbrook" in a tab
 * strip says nothing about what the tab is. A town that has not renamed itself
 * — or whose name is the product name — gets it once rather than twice.
 */
export function documentTitle(townName?: string | null): string {
  const name = townName?.trim();
  if (!name || name === PRODUCT_NAME) return PRODUCT_NAME;
  return `${name} · ${PRODUCT_NAME}`;
}

/**
 * useDocumentTitle keeps the tab named after the town.
 *
 * The name arrives from town config after the first paint, and a council
 * member can change it from Town Hall without a reload, so this follows the
 * value rather than being set once at startup.
 */
export function useDocumentTitle(townName?: string | null): void {
  useEffect(() => {
    document.title = documentTitle(townName);
  }, [townName]);
}
