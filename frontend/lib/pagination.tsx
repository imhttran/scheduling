"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";

/** Approx. height of one table body row (see .user-table padding/line-height). */
export const DEFAULT_ROW_HEIGHT = 40;
/** Vertical space inside the scroll area taken by non-body content (header row + margin). */
export const DEFAULT_RESERVE = 48;
/** Keep at least this many rows per page so pages never look empty. */
export const MIN_ROWS = 4;
/**
 * Fraction of the viewport height the grid may occupy before the pager kicks
 * in. Must match the `.table-scroll { max-height: 55vh }` rule in globals.css.
 */
export const HEIGHT_FRACTION = 0.55;

/**
 * Adaptive client-side pagination: the page size grows/shrinks with the
 * viewport height so the grid fills the available vertical space, and the
 * caller only needs to render `Pager` when `hasPages` is true.
 *
 * Pass a fixed `perPage` to keep a constant page size instead.
 */
export function usePager<T>(
  items: T[],
  options?: {
    rowHeight?: number;
    reserve?: number;
    min?: number;
    heightFraction?: number;
    perPage?: number;
  },
) {
  const {
    rowHeight = DEFAULT_ROW_HEIGHT,
    reserve = DEFAULT_RESERVE,
    min = MIN_ROWS,
    heightFraction = HEIGHT_FRACTION,
    perPage,
  } = options ?? {};

  const [viewport, setViewport] = useState(
    typeof window === "undefined" ? 0 : window.innerHeight,
  );
  const [page, setPage] = useState(1);

  useEffect(() => {
    const onResize = () => setViewport(window.innerHeight);
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const rows = useMemo(() => {
    if (perPage) return perPage;
    const avail = viewport * heightFraction - reserve;
    return Math.max(min, Math.floor(avail / rowHeight));
  }, [viewport, heightFraction, reserve, rowHeight, min, perPage]);

  const pageCount = Math.max(1, Math.ceil(items.length / rows));
  const currentPage = Math.min(page, pageCount);
  const pageItems = useMemo(
    () => items.slice((currentPage - 1) * rows, currentPage * rows),
    [items, currentPage, rows],
  );

  return {
    pageItems,
    pageCount,
    currentPage,
    rows,
    setPage,
    resetToFirst: () => setPage(1),
    hasPages: pageCount > 1,
  };
}

/** Adaptive paging combined with click-to-sort on string/numeric columns. */
export function useSortablePage<T>(
  items: T[],
  getValue: (item: T, key: string) => unknown,
  initialSortKey: string,
  options?: {
    rowHeight?: number;
    reserve?: number;
    min?: number;
    heightFraction?: number;
    perPage?: number;
  },
) {
  const [sortBy, setSortBy] = useState<string>(initialSortKey);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");

  const sorted = useMemo(() => {
    const valueAt = (item: T) => String(getValue(item, sortBy) ?? "");
    return [...items].sort((a, b) => {
      const cmp = valueAt(a).localeCompare(valueAt(b), undefined, {
        numeric: true,
      });
      return sortDir === "asc" ? cmp : -cmp;
    });
  }, [items, getValue, sortBy, sortDir]);

  const pager = usePager<T>(sorted, options);

  const toggleSort = (key: string) => {
    if (sortBy === key) {
      setSortDir(sortDir === "asc" ? "desc" : "asc");
    } else {
      setSortBy(key);
      setSortDir("asc");
    }
    pager.resetToFirst();
  };

  // Restore the default sort and page (used after adding/reloading data).
  const reset = () => {
    setSortBy(initialSortKey);
    setSortDir("asc");
    pager.resetToFirst();
  };

  return { ...pager, sortBy, sortDir, toggleSort, reset };
}

/** Prev/Next pager. Renders nothing when there is only one page. */
export function Pager({
  pageCount,
  currentPage,
  onPrev,
  onNext,
}: {
  pageCount: number;
  currentPage: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  if (pageCount <= 1) return null;
  return (
    <div className="user-pager">
      <button type="button" disabled={currentPage <= 1} onClick={onPrev}>
        Prev
      </button>
      <span>
        Page {currentPage} of {pageCount}
      </span>
      <button
        type="button"
        disabled={currentPage >= pageCount}
        onClick={onNext}
      >
        Next
      </button>
    </div>
  );
}

/** Sortable column header that renders a ▲/▼ indicator for the active column. */
export function SortableTh({
  label,
  sortKey,
  sortBy,
  sortDir,
  onSort,
}: {
  label: ReactNode;
  sortKey: string;
  sortBy: string;
  sortDir: "asc" | "desc";
  onSort: () => void;
}) {
  return (
    <th className="sortable" onClick={onSort}>
      {label}
      {sortBy === sortKey ? (sortDir === "asc" ? " ▲" : " ▼") : ""}
    </th>
  );
}
