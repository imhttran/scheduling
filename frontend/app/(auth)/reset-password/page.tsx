"use client";

import { useState, type FormEvent } from "react";
import { confirmedPasswordOrAlert, submitForm } from "@/lib/forms";
import { PageTitle } from "@/components/PageTitle";

export default function ResetPasswordPage() {
  const [busy, setBusy] = useState(false);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const password = String(data.get("password"));
    const confirmPassword = String(data.get("confirmPassword"));

    if (!confirmedPasswordOrAlert(password, confirmPassword)) return;

    void submitForm<{ token: string; message: string }>(
      "/api/reset-password",
      {
        token: new URLSearchParams(window.location.search).get("token"),
        password,
      },
      {
        onSuccess: (result) => {
          localStorage.setItem("auth_token", result.token);
          alert(result.message);
          window.location.href = "/dashboard";
        },
      },
      setBusy,
    );
  };

  return (
    <div className="login-container">
      <PageTitle title="Reset Password | Frontend Template" />
      <form className="login-form" onSubmit={handleSubmit}>
        <h1>Reset Password</h1>
        <p>Choose a new password</p>

        <div className="input-group">
          <label htmlFor="password">New Password</label>
          <input
            type="password"
            id="password"
            name="password"
            placeholder="Min 8 chars, 1 upper, 1 num, 1 special"
            required
          />
        </div>

        <div className="input-group">
          <label htmlFor="confirm-password">Confirm Password</label>
          <input
            type="password"
            id="confirm-password"
            name="confirmPassword"
            placeholder="Re-enter your password"
            required
          />
        </div>

        <button type="submit" className="login-button" disabled={busy}>
          {busy ? "Resetting..." : "Reset Password"}
        </button>

        <div className="form-footer">
          <p>
            <a href="/">Back to login</a>
          </p>
        </div>
      </form>
    </div>
  );
}
