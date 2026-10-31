import { useCallback, useEffect, useRef, useState } from "react";
import ReconnectingWebSocket from "reconnecting-websocket";
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
  const socketRef = useRef<ReconnectingWebSocket | null>(null);

  useEffect(() => {
    let stopped = false;
    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const socket = new ReconnectingWebSocket(
      `${protocol}://${window.location.host}/ws`,
      [],
      {
        minReconnectionDelay: 400,
        maxReconnectionDelay: 5000,
        reconnectionDelayGrowFactor: 2,
        minUptime: 0,
        connectionTimeout: 4000,
        maxRetries: Infinity,
        maxEnqueuedMessages: 0,
      },
    );
    socketRef.current = socket;

    socket.onopen = () => {
      if (!stopped) setState("open");
    };
    socket.onmessage = (event) => {
      try {
        handlerRef.current(JSON.parse(event.data) as WsServerMessage);
      } catch {
        // Ignore malformed frames.
      }
    };
    socket.onclose = () => {
      if (!stopped) setState("closed");
    };

    return () => {
      stopped = true;
      socketRef.current = null;
      socket.close();
    };
  }, []);

  const send = useCallback((message: unknown): boolean => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== ReconnectingWebSocket.OPEN) return false;
    try {
      socket.send(JSON.stringify(message));
      return true;
    } catch {
      return false;
    }
  }, []);

  return { state, send };
}
