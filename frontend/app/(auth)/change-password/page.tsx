"use client";

import { useState, type FormEvent } from "react";
import { confirmedPasswordOrAlert, submitAuthedForm } from "@/lib/forms";
import { PageTitle } from "@/components/PageTitle";

export default function ChangePasswordPage() {
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const password = String(data.get("password"));
    const confirmPassword = String(data.get("confirmPassword"));

    if (!confirmedPasswordOrAlert(password, confirmPassword)) return;

    void submitAuthedForm("/api/change-password", {
      currentPassword: data.get("currentPassword"),
      newPassword: password,
    });
  };

  return (
    <div className="login-container">
      <PageTitle title="Change Password | Frontend Template" />
      <form className="login-form" onSubmit={handleSubmit}>
        <h1>Change Password</h1>
        <p>Choose a new password</p>

        <div className="input-group">
          <label htmlFor="current-password">Current Password</label>
          <input
            type="password"
            id="current-password"
            name="currentPassword"
            placeholder="••••••••"
            required
          />
        </div>

        <div className="input-group">
          <label htmlFor="new-password">New Password</label>
          <input
            type="password"
            id="new-password"
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

        <button type="submit" className="login-button">
          Change Password
        </button>
      </form>
    </div>
  );
}
