"use client";

import {
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { confirmedPasswordOrAlert, submitForm } from "@/lib/forms";
import { PageTitle } from "@/components/PageTitle";

type LoginResult = { token: string; twoFactorRequired?: boolean };

// A stable per-browser device id, used to skip 2FA on trusted devices.
function getDeviceId(): string {
  let id = localStorage.getItem("device_id");
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem("device_id", id);
  }
  return id;
}

export default function LoginPage() {
  const [mode, setMode] = useState<"login" | "signup">("login");
  const [busy, setBusy] = useState(false);
  const [pendingToken, setPendingToken] = useState<string | null>(null);
  const [digits, setDigits] = useState(["", "", "", ""]);
  const [resends, setResends] = useState(0);
  const digitRefs = useRef<(HTMLInputElement | null)[]>([]);

  const verifyWithCode = (code: string) => {
    if (!pendingToken) return;
    void submitForm<LoginResult>(
      "/api/login/verify",
      { token: pendingToken, code, deviceId: getDeviceId() },
      {
        onSuccess: (result) => {
          localStorage.setItem("auth_token", result.token);
          window.location.href = "/dashboard";
        },
      },
      setBusy,
    );
  };

  // Auto-verify once all four digits are filled.
  useEffect(() => {
    if (digits.every((d) => d !== "")) {
      verifyWithCode(digits.join(""));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [digits]);

  const handleDigitChange = (index: number, value: string) => {
    const digit = value.replace(/\D/g, "").slice(-1);
    const next = [...digits];
    next[index] = digit;
    setDigits(next);
    if (digit && index < 3) digitRefs.current[index + 1]?.focus();
  };

  const handlePaste = (e: ClipboardEvent<HTMLInputElement>) => {
    e.preventDefault();
    const pasted = e.clipboardData
      .getData("text")
      .replace(/\D/g, "")
      .slice(0, 4);
    const next = ["", "", "", ""];
    for (let i = 0; i < pasted.length; i++) next[i] = pasted[i];
    setDigits(next);
    digitRefs.current[Math.min(pasted.length, 3)]?.focus();
  };

  const handleKeyDown = (index: number, e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Backspace" && !digits[index] && index > 0) {
      digitRefs.current[index - 1]?.focus();
    }
  };

  const handleResend = () => {
    if (!pendingToken || resends >= 3) return;
    void submitForm(
      "/api/login/resend",
      { token: pendingToken },
      {
        onSuccess: () => {
          setResends((n) => n + 1);
          setDigits(["", "", "", ""]);
          digitRefs.current[0]?.focus();
        },
      },
      setBusy,
    );
  };

  const handleCancelVerify = () => {
    setPendingToken(null);
    setDigits(["", "", "", ""]);
    setResends(0);
  };

  const handleLogin = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    void submitForm<LoginResult>(
      "/api/login",
      {
        email: data.get("email"),
        password: data.get("password"),
        deviceId: getDeviceId(),
      },
      {
        onSuccess: (result) => {
          if (result.twoFactorRequired) {
            setPendingToken(result.token);
          } else {
            localStorage.setItem("auth_token", result.token);
            window.location.href = "/dashboard";
          }
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
      {pendingToken ? (
        <form className="login-form" autoComplete="off">
          <h1>Verify It&apos;s You</h1>
          <p>Enter the 4-digit code sent to your device</p>

          <div className="input-group">
            <label htmlFor="code-0">Verification Code</label>
            <div className="code-inputs">
              {digits.map((d, i) => (
                <input
                  key={i}
                  ref={(el) => {
                    digitRefs.current[i] = el;
                  }}
                  id={`code-${i}`}
                  type="text"
                  inputMode="numeric"
                  pattern="[0-9]"
                  maxLength={1}
                  autoComplete="one-time-code"
                  autoFocus={i === 0}
                  value={d}
                  disabled={busy}
                  onChange={(e) => handleDigitChange(i, e.target.value)}
                  onPaste={handlePaste}
                  onKeyDown={(e) => handleKeyDown(i, e)}
                />
              ))}
            </div>
            <div className="form-footer">
              <p>
                Didn&apos;t get it?{" "}
                <a
                  href="#"
                  onClick={(e) => {
                    e.preventDefault();
                    handleResend();
                  }}
                >
                  Resend code
                </a>
                {resends > 0 && ` (${3 - resends} left)`}
              </p>
              <p>
                <a
                  href="#"
                  onClick={(e) => {
                    e.preventDefault();
                    handleCancelVerify();
                  }}
                >
                  Cancel
                </a>
              </p>
            </div>
          </div>
        </form>
      ) : mode === "login" ? (
        <form className="login-form" onSubmit={handleLogin}>
          <h1>Welcome Back</h1>
          <p>Please enter your details</p>

          <div className="input-group">
            <label htmlFor="email">Email or UID</label>
            <input
              type="text"
              id="email"
              name="email"
              placeholder="Enter your email or UID"
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
