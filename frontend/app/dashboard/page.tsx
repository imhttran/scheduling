"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
  type MouseEvent,
  type ReactEventHandler,
} from "react";
import { API_BASE, callApi } from "@/lib/api";
import { ROLES, hasRole } from "@/lib/roles";
import { PageHeader } from "@/components/PageHeader";
import { PageFooter } from "@/components/PageFooter";
import { PageTitle } from "@/components/PageTitle";

const USERS_PER_PAGE = 10;

const yesNo = (value: boolean) => (value ? "Yes" : "No");

type MeUser = {
  id: number;
  email: string;
  role: string;
  emailVerified: boolean;
  mustChangePassword?: boolean;
  hasProfile?: boolean;
};

type UserRow = {
  id: number;
  email: string;
  role: string;
  emailVerified: boolean;
};

type SortKey = "email" | "role" | "emailVerified";
type SortDir = "asc" | "desc";

const SORTABLE_COLUMNS: { key: SortKey; label: string }[] = [
  { key: "email", label: "Email" },
  { key: "role", label: "Role" },
  { key: "emailVerified", label: "Verified" },
];

type TableAction = { label: string; onClick: () => void; danger?: boolean };

// A single action renders as its own button (unchanged behavior). More than
// one becomes a real floating dropdown via the native Popover API — it renders
// in the browser's top layer, so it's never clipped by .table-scroll's
// overflow and doesn't push the table's rows around when opened.
// Outside-click/Escape dismissal is native; only positioning and
// closing-on-item-click are ours.
function ActionsCell({ actions }: { actions: TableAction[] }) {
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);

  if (actions.length <= 1) {
    return (
      <>
        {actions.map((action) => (
          <button key={action.label} type="button" onClick={action.onClick}>
            {action.label}
          </button>
        ))}
      </>
    );
  }

  const toggleMenu = () => {
    const menu = menuRef.current;
    const trigger = triggerRef.current;
    if (!menu || !trigger) return;
    const rect = trigger.getBoundingClientRect();
    menu.style.top = `${rect.bottom + 4}px`;
    menu.style.left = `${rect.left}px`;
    menu.togglePopover();
  };

  const handleToggle: ReactEventHandler<HTMLDivElement> = (event) => {
    const isOpen =
      event.nativeEvent instanceof window.ToggleEvent &&
      event.nativeEvent.newState === "open";
    setOpen(isOpen);
    if (isOpen) {
      // Popovers don't reposition on scroll — close instead of drifting
      // away from the trigger that opened them.
      window.addEventListener("scroll", () => menuRef.current?.hidePopover(), {
        once: true,
        capture: true,
      });
    }
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="actions-trigger"
        onClick={toggleMenu}
      >
        {open ? "Actions ▴" : "Actions ▾"}
      </button>
      <div
        ref={menuRef}
        popover="auto"
        className="actions-menu-list"
        onToggle={handleToggle}
      >
        {actions.map((action) => (
          <button
            key={action.label}
            type="button"
            className={
              action.danger ? "link-button button-danger" : "link-button"
            }
            onClick={() => {
              menuRef.current?.hidePopover();
              action.onClick();
            }}
          >
            {action.label}
          </button>
        ))}
      </div>
    </>
  );
}

export default function DashboardPage() {
  const [me, setMe] = useState<MeUser | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [users, setUsers] = useState<UserRow[] | null>(null);
  const [usersFailed, setUsersFailed] = useState(false);
  // Sort column/direction + current page survive across fetches so a
  // mutation's refresh doesn't reset the admin's place in the list.
  const [sortBy, setSortBy] = useState<SortKey>("email");
  const [sortDir, setSortDir] = useState<SortDir>("asc");
  const [page, setPage] = useState(1);
  const addUserDetailsRef = useRef<HTMLDetailsElement>(null);

  const isAdmin = me ? hasRole(me.role, "admin") : false;
  const isStaff = me ? hasRole(me.role, "staff") : false;

  const loadUsers = useCallback(async (authToken: string) => {
    try {
      const response = await fetch(`${API_BASE}/api/users`, {
        headers: { Authorization: `Bearer ${authToken}` },
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.message);
      if (!data.users) throw new Error("No users in response");
      setUsers(data.users);
      setUsersFailed(false);
    } catch {
      setUsersFailed(true);
    }
  }, []);

  useEffect(() => {
    (async () => {
      const stored = localStorage.getItem("auth_token");

      if (!stored) {
        // No token? Kick them back to login
        window.location.href = "/";
        return;
      }
      setToken(stored);

      try {
        // notify=false: this is the page's own auto-load, not an action
        // the user requested — redirect on failure instead of alerting.
        const result = await callApi<{ user: MeUser }>(
          stored,
          "/api/me",
          "GET",
          undefined,
          false,
        );
        if (!result) {
          // Token expired or invalid? Clear it and kick back to login.
          localStorage.removeItem("auth_token");
          window.location.href = "/";
          return;
        }
        const user = result.user;
        // Admin-created accounts start with a temp password — force a change
        // before anything else in the dashboard is usable.
        if (user.mustChangePassword) {
          window.location.href = "/change-password";
          return;
        }
        // New accounts (self-signup or admin-created) start with no profile —
        // send them to fill it in before anything else in the dashboard loads.
        if (!user.hasProfile) {
          window.location.href = "/profile";
          return;
        }
        setMe(user);

        // Route by role: students/managers/admins get their own schedule pages.
        if (user.role === "student") {
          window.location.href = "/student";
          return;
        }
        if (user.role === "manager" || user.role === "scheduler") {
          window.location.href = "/manager";
          return;
        }
        if (user.role === "admin") {
          window.location.href = "/admin";
          return;
        }

        // Only staff/admin can list users at all (backend enforces this too).
        if (hasRole(user.role, "staff")) await loadUsers(stored);
      } catch {
        window.location.href = "/";
      }
    })();
  }, [loadUsers]);

  // Sorting and paging just re-run against the in-memory list; only a real
  // mutation re-fetches (via loadUsers), preserving the current view.
  const sorted = users
    ? [...users].sort((a, b) => {
        const cmp = String(a[sortBy]).localeCompare(
          String(b[sortBy]),
          undefined,
          { numeric: true },
        );
        return sortDir === "asc" ? cmp : -cmp;
      })
    : null;

  const pageCount = Math.max(
    1,
    Math.ceil((sorted?.length ?? 0) / USERS_PER_PAGE),
  );
  const currentPage = Math.min(page, pageCount);
  const pageUsers = sorted
    ? sorted.slice(
        (currentPage - 1) * USERS_PER_PAGE,
        currentPage * USERS_PER_PAGE,
      )
    : [];

  const toggleSort = (key: SortKey) => {
    if (sortBy === key) {
      setSortDir(sortDir === "asc" ? "desc" : "asc");
    } else {
      setSortBy(key);
      setSortDir("asc");
    }
    setPage(1);
  };

  const withToken = useCallback(
    (run: (authToken: string) => Promise<void>) => {
      if (!token) return;
      void run(token);
    },
    [token],
  );

  const handleAddUser = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!token) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    void (async () => {
      const result = await callApi(token, "/api/users", "POST", {
        email: data.get("email"),
        password: data.get("password"),
      });
      if (result) {
        form.reset();
        if (addUserDetailsRef.current) addUserDetailsRef.current.open = false;
        setSortBy("email");
        setSortDir("asc");
        setPage(1);
        await loadUsers(token);
      }
    })();
  };

  const logout = (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    localStorage.removeItem("auth_token");
    window.location.href = "/";
  };

  return (
    <div
      className={isStaff ? "dashboard-container wide" : "dashboard-container"}
    >
      <PageTitle title="Dashboard | Frontend Template" />
      <PageHeader
        title="Dashboard"
        subtitle={
          <>
            Welcome back,{" "}
            <span id="user-email" className="highlight">
              {me?.email ?? "..."}
            </span>
            !
          </>
        }
      >
        <a className="logout-link" href="/" onClick={logout}>
          Logout
        </a>
      </PageHeader>

      <div className="dashboard-card">
        {isStaff && (
          <div className="user-list-section">
            <h2>Users</h2>
            {isAdmin && (
              <details ref={addUserDetailsRef}>
                <summary className="add-user-toggle">Add User</summary>
                <form className="add-user-form" onSubmit={handleAddUser}>
                  <input
                    type="email"
                    name="email"
                    placeholder="Email"
                    required
                  />
                  <input
                    type="password"
                    name="password"
                    placeholder="Temporary password"
                    required
                  />
                  <button type="submit" className="login-button">
                    Add User
                  </button>
                </form>
              </details>
            )}
            <div className="table-scroll">
              <table className="user-table">
                <thead>
                  <tr>
                    {SORTABLE_COLUMNS.map((column) => (
                      <th
                        key={column.key}
                        className="sortable"
                        onClick={() => toggleSort(column.key)}
                      >
                        {column.label}
                        {sortBy === column.key
                          ? sortDir === "asc"
                            ? " ▲"
                            : " ▼"
                          : ""}
                      </th>
                    ))}
                    <th style={isAdmin ? undefined : { display: "none" }}>
                      Actions
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {usersFailed ? (
                    <tr>
                      <td colSpan={4}>Failed to load users.</td>
                    </tr>
                  ) : (
                    pageUsers.map((user) => {
                      const actions: TableAction[] = [];
                      if (!user.emailVerified) {
                        actions.push({
                          label: "Resend Verification",
                          onClick: () =>
                            withToken((authToken) =>
                              callApi(
                                authToken,
                                `/api/users/${user.id}/resend-verification`,
                                "POST",
                              ).then(() => undefined),
                            ),
                        });
                      }
                      if (isAdmin) {
                        actions.push({
                          label: user.emailVerified ? "Unverify" : "Verify",
                          onClick: () =>
                            withToken(async (authToken) => {
                              await callApi(
                                authToken,
                                `/api/users/${user.id}/verification`,
                                "PATCH",
                                { emailVerified: !user.emailVerified },
                              );
                              await loadUsers(authToken);
                            }),
                        });
                        actions.push({
                          label: "Reset Password",
                          onClick: () =>
                            withToken((authToken) =>
                              callApi(
                                authToken,
                                `/api/users/${user.id}/reset-password`,
                                "POST",
                              ).then(() => undefined),
                            ),
                        });
                        actions.push({
                          label: "Delete",
                          danger: true,
                          onClick: () => {
                            if (!window.confirm(`Delete ${user.email}?`))
                              return;
                            withToken(async (authToken) => {
                              await callApi(
                                authToken,
                                `/api/users/${user.id}`,
                                "DELETE",
                              );
                              await loadUsers(authToken);
                            });
                          },
                        });
                      }

                      return (
                        <tr key={user.id}>
                          <td>{user.email}</td>
                          <td>
                            {/* Admins get an editable dropdown (except on their
                                own row — the backend also blocks self-demotion,
                                but disabling here skips the round trip). */}
                            {isAdmin && user.id !== me?.id ? (
                              <select
                                key={user.role}
                                defaultValue={user.role}
                                onChange={(
                                  event: ChangeEvent<HTMLSelectElement>,
                                ) =>
                                  withToken(async (authToken) => {
                                    await callApi(
                                      authToken,
                                      `/api/users/${user.id}/role`,
                                      "PATCH",
                                      { role: event.target.value },
                                    );
                                    await loadUsers(authToken);
                                  })
                                }
                              >
                                {ROLES.map((role) => (
                                  <option key={role} value={role}>
                                    {role}
                                  </option>
                                ))}
                              </select>
                            ) : (
                              user.role
                            )}
                          </td>
                          <td>{yesNo(user.emailVerified)}</td>
                          <td>
                            <ActionsCell actions={actions} />
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </div>
            {pageCount > 1 && (
              <div className="user-pager">
                <button
                  type="button"
                  disabled={currentPage <= 1}
                  onClick={() => setPage(currentPage - 1)}
                >
                  Prev
                </button>
                <span>
                  Page {currentPage} of {pageCount}
                </span>
                <button
                  type="button"
                  disabled={currentPage >= pageCount}
                  onClick={() => setPage(currentPage + 1)}
                >
                  Next
                </button>
              </div>
            )}
          </div>
        )}
      </div>

      <PageFooter
        meta={
          <span>
            Role:{" "}
            <span id="user-role" className="highlight">
              {me?.role ?? "..."}
            </span>{" "}
            · Email verified:{" "}
            <span id="user-verified" className="highlight">
              {me ? yesNo(me.emailVerified) : "..."}
            </span>
          </span>
        }
      />
    </div>
  );
}
