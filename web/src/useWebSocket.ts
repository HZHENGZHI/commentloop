import { useCallback, useEffect, useRef, useState } from "react";
import type { WsServerMessage } from "./types";

export type ConnectionState = "connecting" | "open" | "closed";

interface UseWebSocket {
  state: ConnectionState;
  send: (message: unknown) => boolean;
}

/**
 * Maintains a resilient WebSocket connection to the Go backend and always
 * dispatches messages to the latest handler.
 */
export function useWebSocket(
  onMessage: (message: WsServerMessage) => void,
): UseWebSocket {
  const handlerRef = useRef(onMessage);
  handlerRef.current = onMessage;

  const [state, setState] = useState<ConnectionState>("connecting");
  const socketRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let stopped = false;
    let retry = 0;
    let timer: number | undefined;

    function schedule() {
      if (stopped) return;
      const delay = Math.min(5000, 400 * 2 ** Math.min(retry, 4));
      retry += 1;
      timer = window.setTimeout(connect, delay);
    }

    function connect() {
      if (stopped) return;
      setState("connecting");
      const protocol = window.location.protocol === "https:" ? "wss" : "ws";
      let socket: WebSocket;
      try {
        socket = new WebSocket(`${protocol}://${window.location.host}/ws`);
      } catch {
        schedule();
        return;
      }
      socketRef.current = socket;

      socket.onopen = () => {
        retry = 0;
        setState("open");
      };
      socket.onmessage = (event) => {
        try {
          handlerRef.current(JSON.parse(event.data) as WsServerMessage);
        } catch {
          // Ignore malformed frames.
        }
      };
      socket.onclose = () => {
        socketRef.current = null;
        setState("closed");
        schedule();
      };
      socket.onerror = () => {
        try {
          socket.close();
        } catch {
          // Nothing to do.
        }
      };
    }

    connect();

    return () => {
      stopped = true;
      if (timer !== undefined) window.clearTimeout(timer);
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, []);

  const send = useCallback((message: unknown): boolean => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return false;
    try {
      socket.send(JSON.stringify(message));
      return true;
    } catch {
      return false;
    }
  }, []);

  return { state, send };
}
