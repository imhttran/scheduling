"use client";

import { useState, type FormEvent } from "react";
import { Modal } from "./Modal";
import { fmtTime } from "./WeekCalendar";

const CANNED_REASONS = [
  "Class conflict",
  "Doctor's appointment",
  "Family event",
  "Exam preparation",
  "Traveling",
  "Other...",
];

// Modal for requesting to miss an assigned shift: pick a canned reason or type
// a freeform one.
export function MissRequestModal({
  shift,
  onClose,
  onSubmit,
}: {
  shift: {
    id: number;
    date: string;
    startTime: string;
    endTime: string;
  } | null;
  onClose: () => void;
  onSubmit: (reason: string) => void;
}) {
  const [reason, setReason] = useState(CANNED_REASONS[0]);
  const [custom, setCustom] = useState("");

  if (!shift) return null;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const finalReason = reason === "Other..." ? custom.trim() : reason;
    onSubmit(finalReason || "Unavailable");
  };

  return (
    <Modal title="Request to miss shift" onClose={onClose}>
      <form className="modal-form" onSubmit={submit}>
        <p className="section-hint">
          {shift.date} · {fmtTime(shift.startTime)}–{fmtTime(shift.endTime)}
        </p>
        <label className="modal-label">
          Reason
          <select value={reason} onChange={(e) => setReason(e.target.value)}>
            {CANNED_REASONS.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>
        {reason === "Other..." && (
          <label className="modal-label">
            Custom reason
            <input
              type="text"
              value={custom}
              onChange={(e) => setCustom(e.target.value)}
              placeholder="Tell us why"
              required
            />
          </label>
        )}
        <div className="modal-actions">
          <button type="button" className="cancel-button" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="login-button">
            Submit
          </button>
        </div>
      </form>
    </Modal>
  );
}
