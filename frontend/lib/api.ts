// Low-level API transport. Same-origin by default: the frontend's own server
// proxies /api/* to the Go API (see next.config.ts), so the browser never
// talks to the API directly. NEXT_PUBLIC_API_URL overrides this for a
// split-domain deployment that calls the Go API directly (CORS must then be
// enabled on the Go side).
export const API_BASE = process.env.NEXT_PUBLIC_API_URL || "";

export type ApiResult = { message?: string; [key: string]: unknown };

// Every authenticated admin/staff/self-service action hits the API through this
// helper: it sends the token in the Authorization header, logs the response
// message (unless notify=false) instead of popping an alert, and surfaces
// network errors the same way every other fetch does (rather than an unhandled
// rejection).
export async function callApi<T extends ApiResult = ApiResult>(
  token: string,
  path: string,
  method: string,
  body?: unknown,
  notify = true,
): Promise<T | false> {
  try {
    const response = await fetch(`${API_BASE}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body ? { "Content-Type": "application/json" } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    const result = (await response.json()) as T;
    if (notify) console.log(`[api] ${result.message}`);
    return response.ok ? result : false;
  } catch {
    if (notify) alert("Connection error. Is the backend running?");
    return false;
  }
}
