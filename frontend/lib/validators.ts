// Shared validation utilities (port of common/validators.js)

export function validateEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return emailRegex.test(email);
}

// US phone numbers only, digits with optional standard formatting
// (spaces/dots/dashes/parens) and an optional leading +1/1.
export function validatePhone(phone: string): boolean {
  const phoneRegex = /^\+?1?[-.\s]?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}$/;
  return phoneRegex.test(phone);
}

// US zip codes only — 5 digits, matching the profile form's pattern="[0-9]{5}"
// (no ZIP+4 support yet).
export function validateZip(zip: string): boolean {
  return /^\d{5}$/.test(zip);
}

// http(s) only — good enough for LinkedIn/GitHub profile links. Uses the
// native URL parser instead of a hand-rolled regex.
export function validateUrl(url: string): boolean {
  try {
    return ["http:", "https:"].includes(new URL(url).protocol);
  } catch {
    return false;
  }
}

// Returns an error message describing the first unmet rule, or null if valid.
export function validatePassword(password: string): string | null {
  if (!password || password.length < 8) {
    return "Password must be at least 8 characters long";
  }
  if (!/[A-Z]/.test(password)) {
    return "Password must contain at least one uppercase letter";
  }
  if (!/\d/.test(password)) {
    return "Password must contain at least one number";
  }
  if (!/[!@#$%^&*(),.?":{}|<>]/.test(password)) {
    return "Password must contain at least one special character";
  }
  return null;
}
