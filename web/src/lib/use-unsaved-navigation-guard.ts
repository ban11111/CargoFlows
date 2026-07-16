"use client";

import { useEffect, useRef } from "react";

interface NavigationDestinationLike {
  url: string;
}

interface NavigateEventLike extends Event {
  destination?: NavigationDestinationLike;
}

interface NavigationLike {
  addEventListener(type: "navigate", listener: EventListener): void;
  removeEventListener(type: "navigate", listener: EventListener): void;
}

function currentNavigation(): NavigationLike | undefined {
  return (window as Window & { navigation?: NavigationLike }).navigation;
}

function comparableURL(url: URL) {
  return `${url.origin}${url.pathname}${url.search}`;
}

function isSamePage(destination: URL) {
  return comparableURL(destination) === comparableURL(new URL(window.location.href));
}

export function useUnsavedNavigationGuard(active: boolean, message: string) {
  const messageRef = useRef(message);

  useEffect(() => {
    messageRef.current = message;
  }, [message]);

  useEffect(() => {
    if (!active) return;

    // Navigation API can cancel traversals without rewriting the user's stack.
    // Unsupported browsers retain link + beforeunload protection; popstate is intentionally not simulated.
    const navigation = currentNavigation();
    let bypassNextClick = false;
    let allowedNavigationURL: string | null = null;

    function beforeUnload(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
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
      if (destination.origin !== window.location.origin || isSamePage(destination)) return;

      event.preventDefault();
      event.stopImmediatePropagation();
      if (!window.confirm(messageRef.current)) return;

      const confirmedURL = navigation ? comparableURL(destination) : null;
      allowedNavigationURL = confirmedURL;
      bypassNextClick = true;
      anchor.click();
      queueMicrotask(() => {
        if (allowedNavigationURL === confirmedURL) allowedNavigationURL = null;
      });
    }

    function navigationGuard(rawEvent: Event) {
      const event = rawEvent as NavigateEventLike;
      if (!event.destination) return;
      const destination = new URL(event.destination.url, window.location.href);
      if (destination.origin !== window.location.origin || isSamePage(destination)) return;

      const comparableDestination = comparableURL(destination);
      if (allowedNavigationURL === comparableDestination) {
        allowedNavigationURL = null;
        return;
      }
      if (!window.confirm(messageRef.current)) event.preventDefault();
    }

    window.addEventListener("beforeunload", beforeUnload);
    document.addEventListener("click", clickGuard, true);
    navigation?.addEventListener("navigate", navigationGuard);

    return () => {
      window.removeEventListener("beforeunload", beforeUnload);
      document.removeEventListener("click", clickGuard, true);
      navigation?.removeEventListener("navigate", navigationGuard);
    };
  }, [active]);
}
