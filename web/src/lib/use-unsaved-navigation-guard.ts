"use client";

import { useEffect, useId } from "react";

const sentinelKey = "__cargoFlowUnsavedNavigationGuard";

export function useUnsavedNavigationGuard(active: boolean, message: string) {
  const marker = useId();

  useEffect(() => {
    if (!active) return;

    let sentinelActive = true;
    let restoringSentinel = false;
    let allowingNextPop = false;
    let disarmingSentinel = false;
    let bypassNextClick = false;
    let disposed = false;

    const state = typeof window.history.state === "object" && window.history.state !== null ? window.history.state : {};
    window.history.pushState({ ...state, [sentinelKey]: marker }, "", window.location.href);

    const hasCurrentSentinel = () => window.history.state?.[sentinelKey] === marker;

    function beforeUnload(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
    }

    function stopClick(event: MouseEvent) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }

    function disarmSentinel(continuation?: () => void) {
      if (!sentinelActive || !hasCurrentSentinel()) {
        sentinelActive = false;
        continuation?.();
        return;
      }
      disarmingSentinel = true;
      window.addEventListener("popstate", () => {
        disarmingSentinel = false;
        sentinelActive = false;
        if (!disposed) continuation?.();
      }, { once: true });
      window.history.back();
    }

    function clickGuard(event: MouseEvent) {
      if (bypassNextClick) {
        bypassNextClick = false;
        return;
      }
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const target = event.target;
      const anchor = target instanceof Element ? target.closest<HTMLAnchorElement>("a[href]") : null;
      if (!anchor || anchor.hasAttribute("download") || (anchor.target && anchor.target !== "_self")) return;
      const destination = new URL(anchor.href, window.location.href);
      if (destination.origin !== window.location.origin) return;
      if (destination.pathname === window.location.pathname && destination.search === window.location.search) return;

      stopClick(event);
      if (!window.confirm(message)) return;
      disarmSentinel(() => {
        bypassNextClick = true;
        anchor.click();
      });
    }

    function popGuard() {
      if (disarmingSentinel) return;
      if (restoringSentinel) {
        restoringSentinel = false;
        return;
      }
      if (allowingNextPop) {
        allowingNextPop = false;
        sentinelActive = false;
        return;
      }
      if (window.confirm(message)) {
        allowingNextPop = true;
        sentinelActive = false;
        window.history.back();
        return;
      }
      restoringSentinel = true;
      window.history.forward();
    }

    window.addEventListener("beforeunload", beforeUnload);
    document.addEventListener("click", clickGuard, true);
    window.addEventListener("popstate", popGuard);

    return () => {
      disposed = true;
      window.removeEventListener("beforeunload", beforeUnload);
      document.removeEventListener("click", clickGuard, true);
      window.removeEventListener("popstate", popGuard);
      if (sentinelActive && hasCurrentSentinel()) window.history.back();
    };
  }, [active, marker, message]);
}
