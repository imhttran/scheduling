"use client";

import { useState, type FormEvent } from "react";
import { submitEmailForm } from "@/lib/forms";
import { PageTitle } from "@/components/PageTitle";

export default function ResendVerificationPage() {
  const [busy, setBusy] = useState(false);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void submitEmailForm(
      event.currentTarget,
      "/api/resend-verification",
      setBusy,
    );
  };

  return (
    <div className="login-container">
      <PageTitle title="Resend Verification | Frontend Template" />
      <form className="login-form" onSubmit={handleSubmit}>
        <h1>Resend Verification</h1>
        <p>Enter your email and we&apos;ll send a new verification link</p>

        <div className="input-group">
          <label htmlFor="email">Email</label>
          <input
            type="email"
            id="email"
            name="email"
            placeholder="Enter your email"
            required
          />
        </div>

        <button type="submit" className="login-button" disabled={busy}>
          {busy ? "Sending..." : "Resend Verification"}
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
