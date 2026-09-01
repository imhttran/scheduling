"use client";

import type { ReactNode } from "react";
import { Pager, SortableTh } from "@/lib/pagination";

/** A single grid column. `sortable` uses `key` as both the identity and the
 *  sort key; `render` supplies a custom cell (buttons, selects, composites,
 *  etc.); without it the cell falls back to the raw `row[key]` value. */
export type Column<T> = {
  key?: string;
  label?: ReactNode;
  sortable?: boolean;
  render?: (row: T) => ReactNode;
};

/** Grid state returned by `useSortablePage`/`usePager` (with optional sorting). */
export type GridState<T> = {
  pageItems: T[];
  pageCount: number;
  currentPage: number;
  setPage: (page: number) => void;
  sortBy?: string;
  sortDir?: "asc" | "desc";
  toggleSort?: (key: string) => void;
};

/**
 * Config-driven data grid: owns the table, sortable headers, empty state,
 * current-page rows, and the pager. Sorting activates automatically when the
 * grid comes from `useSortablePage` (has sortBy/toggleSort); otherwise
 * sortable columns render as plain headers.
 */
export function DataGrid<T>({
  grid,
  columns,
  getRowKey,
  emptyText = "No data.",
}: {
  grid: GridState<T>;
  columns: Column<T>[];
  getRowKey: (row: T) => string | number;
  emptyText?: string;
}) {
  const canSort = Boolean(grid.sortBy && grid.toggleSort);

  return (
    <div className="data-grid">
      <div className="table-scroll">
        <table className="user-table">
          <thead>
            <tr>
              {columns.map((col, i) =>
                col.sortable && canSort && grid.toggleSort ? (
                  <SortableTh
                    key={i}
                    label={col.label ?? ""}
                    sortKey={col.key ?? ""}
                    sortBy={grid.sortBy!}
                    sortDir={grid.sortDir!}
                    onSort={() => grid.toggleSort?.(col.key ?? "")}
                  />
                ) : (
                  <th key={i}>{col.label}</th>
                ),
              )}
            </tr>
          </thead>
          <tbody>
            {grid.pageItems.length === 0 ? (
              <tr>
                <td colSpan={columns.length}>{emptyText}</td>
              </tr>
            ) : (
              grid.pageItems.map((row) => (
                <tr key={getRowKey(row)}>
                  {columns.map((col, i) => (
                    <td key={i}>
                      {col.render
                        ? col.render(row)
                        : String(
                            (row as Record<string, unknown>)[col.key ?? ""] ??
                              "",
                          )}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <Pager
        pageCount={grid.pageCount}
        currentPage={grid.currentPage}
        onPrev={() => grid.setPage(grid.currentPage - 1)}
        onNext={() => grid.setPage(grid.currentPage + 1)}
      />
    </div>
  );
}
