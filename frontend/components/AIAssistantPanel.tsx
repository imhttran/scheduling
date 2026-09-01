"use client";

import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { API_BASE } from "@/lib/api";

// Bundled AI assistant for Admin/Manager screens. The system prompt, provider,
// and model are configured server-side via env vars (AI_PROMPT, AI_BASE_URL,
// AI_API_KEY, AI_MODEL); this panel only sends the user's question. It floats
// over the page the same way Zed's assistant panel does, so it never displaces
// the dense tables below it.
export function AIAssistantPanel({ token }: { token: string }) {
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [messages, setMessages] = useState<
    { role: "user" | "assistant"; content: string }[]
  >([]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // Keep the newest message in view as the conversation grows.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, open]);

  // Focus the composer when the panel opens.
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const send = async (event?: FormEvent) => {
    event?.preventDefault();
    const message = input.trim();
    if (!message || busy) return;

    setMessages((m) => [...m, { role: "user", content: message }]);
    setInput("");
    setBusy(true);
    setError(null);

    try {
      const response = await fetch(`${API_BASE}/api/ai/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ message }),
      });
      const data = (await response.json()) as {
        reply?: string;
        message?: string;
      };
      if (!response.ok) {
        setError(data.message ?? "Something went wrong. Try again.");
        return;
      }
      setMessages((m) => [
        ...m,
        { role: "assistant", content: data.reply ?? "" },
      ]);
    } catch {
      setError("Connection error. Is the backend running?");
    } finally {
      setBusy(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends; Shift+Enter inserts a newline.
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void send();
    }
  };

  return (
    <>
      <button
        type="button"
        className="ai-toggle"
        aria-expanded={open}
        aria-controls="ai-panel"
        onClick={() => setOpen((o) => !o)}
      >
        <span aria-hidden="true">✦</span> AI assistant
      </button>

      {open && (
        <div
          className="ai-panel"
          id="ai-panel"
          role="dialog"
          aria-label="AI assistant"
        >
          <header className="ai-panel-header">
            <span className="ai-panel-title">✨ AI assistant</span>
            <button
              type="button"
              className="ai-close"
              aria-label="Close AI assistant"
              onClick={() => setOpen(false)}
            >
              ×
            </button>
          </header>

          <div className="ai-messages" ref={scrollRef}>
            {messages.length === 0 ? (
              <p className="ai-placeholder">
                Ask this pre-configured AI agent for help with scheduling,
                workers, or anything on this screen.
              </p>
            ) : (
              messages.map((m, i) => (
                <div key={i} className={`ai-msg ai-msg-${m.role}`}>
                  {m.content}
                </div>
              ))
            )}
            {error && <div className="ai-error">{error}</div>}
          </div>

          <form className="ai-form" onSubmit={send}>
            <textarea
              ref={inputRef}
              className="ai-input"
              rows={2}
              placeholder="Ask the AI agent…"
              value={input}
              disabled={busy}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={onKeyDown}
            />
            <button
              type="submit"
              className="ai-send"
              disabled={busy || !input.trim()}
            >
              {busy ? "…" : "Send"}
            </button>
          </form>
        </div>
      )}
    </>
  );
}
