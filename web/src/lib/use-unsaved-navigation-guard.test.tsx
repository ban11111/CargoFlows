import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useUnsavedNavigationGuard } from "./use-unsaved-navigation-guard";

interface NavigateEventInit {
  cancelable?: boolean;
  downloadRequest?: string | null;
  navigationType?: "push" | "replace" | "reload" | "traverse";
  url?: string;
}

class FakeNavigateEvent extends Event {
  downloadRequest: string | null;
  navigationType: NavigateEventInit["navigationType"];
  destination: { url: string; sameDocument: boolean };

  constructor({ cancelable = true, downloadRequest = null, navigationType = "traverse", url = `${window.location.origin}/previous` }: NavigateEventInit = {}) {
    super("navigate", { cancelable });
    this.downloadRequest = downloadRequest;
    this.navigationType = navigationType;
    this.destination = { url, sameDocument: true };
  }
}

class FakeNavigation extends EventTarget {}

function installNavigation(navigation?: FakeNavigation) {
  Object.defineProperty(window, "navigation", { configurable: true, value: navigation });
}

function Harness({ active = true, initialMessage = "first message", onNavigate = () => undefined }: { active?: boolean; initialMessage?: string; onNavigate?: () => void }) {
  const [message, setMessage] = useState(initialMessage);
  useUnsavedNavigationGuard(active, message);
  return <>
    <button onClick={() => setMessage("second message")} type="button">change message</button>
    <a href="/next" onClick={(event) => { event.preventDefault(); onNavigate(); }}>next</a>
  </>;
}

describe("useUnsavedNavigationGuard", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    installNavigation(undefined);
  });

  it("updates the confirmation message without reinstalling listeners or touching history", () => {
    const addWindowListener = vi.spyOn(window, "addEventListener");
    const addDocumentListener = vi.spyOn(document, "addEventListener");
    const push = vi.spyOn(window.history, "pushState");
    const replace = vi.spyOn(window.history, "replaceState");
    const back = vi.spyOn(window.history, "back");
    const forward = vi.spyOn(window.history, "forward");
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<Harness />);
    const windowListenerCount = addWindowListener.mock.calls.length;
    const documentListenerCount = addDocumentListener.mock.calls.length;

    fireEvent.click(screen.getByRole("button", { name: "change message" }));
    expect(confirm).not.toHaveBeenCalled();
    expect(addWindowListener).toHaveBeenCalledTimes(windowListenerCount);
    expect(addDocumentListener).toHaveBeenCalledTimes(documentListenerCount);
    fireEvent.click(screen.getByRole("link", { name: "next" }));

    expect(confirm).toHaveBeenCalledWith("second message");
    expect(push).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    expect(back).not.toHaveBeenCalled();
    expect(forward).not.toHaveBeenCalled();
  });

  it("preserves pre-existing forward history when becoming clean and on unmount", async () => {
    window.history.replaceState({ entry: "start" }, "", "/history-start");
    window.history.pushState({ entry: "forward" }, "", "/history-forward");
    window.history.back();
    await waitFor(() => expect(window.location.pathname).toBe("/history-start"));

    const push = vi.spyOn(window.history, "pushState");
    const replace = vi.spyOn(window.history, "replaceState");
    const back = vi.spyOn(window.history, "back");
    const forward = vi.spyOn(window.history, "forward");
    const originalState = window.history.state;
    const { rerender, unmount } = render(<Harness />);

    rerender(<Harness active={false} />);
    unmount();

    expect(window.history.state).toBe(originalState);
    expect(push).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    expect(back).not.toHaveBeenCalled();
    expect(forward).not.toHaveBeenCalled();

    window.history.forward();
    await waitFor(() => expect(window.location.pathname).toBe("/history-forward"));
    expect(window.history.state).toEqual({ entry: "forward" });
  });

  it("cancels Navigation API Back and confirms Forward without changing history itself", () => {
    const navigation = new FakeNavigation();
    installNavigation(navigation);
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    const back = vi.spyOn(window.history, "back");
    const forward = vi.spyOn(window.history, "forward");
    render(<Harness />);

    const cancelled = new FakeNavigateEvent({ navigationType: "traverse", url: `${window.location.origin}/previous` });
    const confirmed = new FakeNavigateEvent({ navigationType: "traverse", url: `${window.location.origin}/next` });
    navigation.dispatchEvent(cancelled);
    navigation.dispatchEvent(confirmed);

    expect(cancelled.defaultPrevented).toBe(true);
    expect(confirmed.defaultPrevented).toBe(false);
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(back).not.toHaveBeenCalled();
    expect(forward).not.toHaveBeenCalled();
  });

  it("allows a confirmed Next link exactly once when Navigation API follows the click", () => {
    const navigation = new FakeNavigation();
    installNavigation(navigation);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const navigate = vi.fn(() => navigation.dispatchEvent(new FakeNavigateEvent({ navigationType: "push", url: `${window.location.origin}/next` })));
    render(<Harness onNavigate={navigate} />);

    fireEvent.click(screen.getByRole("link", { name: "next" }));

    expect(navigate).toHaveBeenCalledTimes(1);
    expect(confirm).toHaveBeenCalledTimes(1);
  });

  it("bypasses Navigation API download requests", () => {
    const navigation = new FakeNavigation();
    installNavigation(navigation);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<Harness />);

    const download = new FakeNavigateEvent({ downloadRequest: "product-photo.jpg" });
    navigation.dispatchEvent(download);

    expect(confirm).not.toHaveBeenCalled();
    expect(download.defaultPrevented).toBe(false);
  });

  it("bypasses non-cancelable Navigation API events", () => {
    const navigation = new FakeNavigation();
    installNavigation(navigation);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<Harness />);

    const nonCancelable = new FakeNavigateEvent({ cancelable: false });
    navigation.dispatchEvent(nonCancelable);

    expect(confirm).not.toHaveBeenCalled();
    expect(nonCancelable.defaultPrevented).toBe(false);
  });

  it("uses a non-corrupting fallback when Navigation API is unavailable", () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const push = vi.spyOn(window.history, "pushState");
    const back = vi.spyOn(window.history, "back");
    const forward = vi.spyOn(window.history, "forward");
    render(<Harness />);

    window.dispatchEvent(new PopStateEvent("popstate"));

    expect(confirm).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    expect(back).not.toHaveBeenCalled();
    expect(forward).not.toHaveBeenCalled();
    const unload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(unload);
    expect(unload.defaultPrevented).toBe(true);
  });
});
