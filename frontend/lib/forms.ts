// Form-submission helpers built on the API transport (lib/api). Shared preflight
// checks and the busy flag live here so form pages stay thin.

import { API_BASE, callApi, type ApiResult } from "./api";
import { validatePassword } from "./validators";

// Shared by signup and reset-password: alerts and returns false on the first
// broken rule (mismatch, then strength), true if the password is good to submit.
export function confirmedPasswordOrAlert(
  password: string,
  confirmPassword: string,
): boolean {
  if (password !== confirmPassword) {
    alert("Passwords do not match");
    return false;
  }
  const passwordError = validatePassword(password);
  if (passwordError) {
    alert(passwordError);
    return false;
  }
  return true;
}

// Shared form submit: flips the busy flag around a POST of a JSON body and
// alerts the outcome. onSuccess runs only on HTTP ok and gets the parsed
// response; preflight checks (password match, etc.) stay with the caller,
// before submitForm is invoked.
export async function submitForm<T extends ApiResult = ApiResult>(
  path: string,
  body: unknown,
  {
    onSuccess,
  }: { onSuccess: (result: T) => void | Promise<void> },
  setBusy: (busy: boolean) => void,
): Promise<void> {
  setBusy(true);
  try {
    const response = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const result = (await response.json()) as T;
    if (response.ok) {
      await onSuccess(result);
    } else {
      alert(`Error: ${result.message}`);
    }
  } catch {
    alert("Connection error. Is the backend running?");
  } finally {
    setBusy(false);
  }
}

// Shared by forgot-password and resend-verification: both forms just POST an
// email, alert the response, and send the user back to login on success.
export async function submitEmailForm(
  form: HTMLFormElement,
  path: string,
  setBusy: (busy: boolean) => void,
): Promise<void> {
  const email = new FormData(form).get("email");
  await submitForm(
    path,
    { email },
    {
      onSuccess: (result) => {
        alert(result.message);
        window.location.href = "/";
      },
    },
    setBusy,
  );
}

// Shared by every authenticated self-service form (change-password, profile):
// bounces to login if there's no stored session, POSTs via callApi, and
// sends the user to the dashboard on success.
export async function submitAuthedForm(
  path: string,
  body: object,
): Promise<void> {
  const token = localStorage.getItem("auth_token");
  if (!token) {
    window.location.href = "/";
    return;
  }
  const result = await callApi(token, path, "POST", body);
  if (result) window.location.href = "/dashboard";
}
