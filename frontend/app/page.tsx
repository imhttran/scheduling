"use client";

import { useState, type FormEvent } from "react";
import { confirmedPasswordOrAlert, submitForm } from "@/lib/api";
import { PageTitle } from "@/components/PageTitle";

type LoginResult = { token: string };

export default function LoginPage() {
  const [mode, setMode] = useState<"login" | "signup">("login");
  const [busy, setBusy] = useState(false);

  const handleLogin = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    void submitForm<LoginResult>(
      "/api/login",
      { email: data.get("email"), password: data.get("password") },
      {
        busyLabel: "Signing in...",
        onSuccess: (result) => {
          localStorage.setItem("auth_token", result.token);
          window.location.href = "/dashboard";
        },
      },
      setBusy,
    );
  };

  const handleSignup = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const password = String(data.get("password"));
    const confirmPassword = String(data.get("confirmPassword"));

    if (!confirmedPasswordOrAlert(password, confirmPassword)) return;

    void submitForm(
      "/api/signup",
      { email: data.get("email"), password },
      {
        busyLabel: "Registering...",
        onSuccess: () => {
          alert("Account created! You can now log in.");
          setMode("login");
        },
      },
      setBusy,
    );
  };

  return (
    <div className="login-container">
      <PageTitle title="Login | Frontend Template" />
      {mode === "login" ? (
        <form className="login-form" onSubmit={handleLogin}>
          <h1>Welcome Back</h1>
          <p>Please enter your details</p>

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

          <div className="input-group">
            <label htmlFor="password">Password</label>
            <input
              type="password"
              id="password"
              name="password"
              placeholder="••••••••"
              required
            />
          </div>

          <button type="submit" className="login-button" disabled={busy}>
            {busy ? "Signing in..." : "Sign In"}
          </button>

          <div className="form-footer">
            <p>
              Don&apos;t have an account?{" "}
              <a
                href="#"
                onClick={(e) => {
                  e.preventDefault();
                  setMode("signup");
                }}
              >
                Sign up
              </a>
            </p>
            <p>
              <a href="/forgot-password">Forgot password?</a>
            </p>
            <p>
              Didn&apos;t get a verification email?{" "}
              <a href="/resend-verification">Resend it</a>
            </p>
          </div>
        </form>
      ) : (
        <form className="login-form" onSubmit={handleSignup}>
          <h1>Create Account</h1>
          <p>Join us today</p>

          <div className="input-group">
            <label htmlFor="signup-email">Email</label>
            <input
              type="email"
              id="signup-email"
              name="email"
              placeholder="Enter your email"
              required
            />
          </div>

          <div className="input-group">
            <label htmlFor="signup-password">Password</label>
            <input
              type="password"
              id="signup-password"
              name="password"
              placeholder="Min 8 chars, 1 upper, 1 num, 1 special"
              required
            />
          </div>

          <div className="input-group">
            <label htmlFor="signup-confirm-password">Confirm Password</label>
            <input
              type="password"
              id="signup-confirm-password"
              name="confirmPassword"
              placeholder="Re-enter your password"
              required
            />
          </div>

          <button type="submit" className="login-button" disabled={busy}>
            {busy ? "Registering..." : "Register"}
          </button>

          <div className="form-footer">
            <p>
              Already have an account?{" "}
              <a
                href="#"
                onClick={(e) => {
                  e.preventDefault();
                  setMode("login");
                }}
              >
                Log in
              </a>
            </p>
          </div>
        </form>
      )}
    </div>
  );
}
