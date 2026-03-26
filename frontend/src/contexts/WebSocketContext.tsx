import React, { createContext, useContext, useEffect, useState, useCallback, ReactNode } from 'react';

interface WebSocketMessage {
  type: 'stats' | 'query' | 'alert' | 'health';
  payload: any;
  timestamp: string;
}

interface WebSocketContextType {
  isConnected: boolean;
  lastMessage: WebSocketMessage | null;
  messages: WebSocketMessage[];
  subscribe: (type: string) => void;
  unsubscribe: (type: string) => void;
}

const WebSocketContext = createContext<WebSocketContextType | undefined>(undefined);

const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws';

export const WebSocketProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [socket, setSocket] = useState<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [lastMessage, setLastMessage] = useState<WebSocketMessage | null>(null);
  const [messages, setMessages] = useState<WebSocketMessage[]>([]);
  const [subscriptions, setSubscriptions] = useState<Set<string>>(new Set());

  useEffect(() => {
    const ws = new WebSocket(WS_URL);

    ws.onopen = () => {
      setIsConnected(true);
      subscriptions.forEach(sub => ws.send(JSON.stringify({ action: 'subscribe', type: sub })));
    };

    ws.onmessage = (event) => {
      try {
        const message: WebSocketMessage = JSON.parse(event.data);
        setLastMessage(message);
        setMessages(prev => [...prev.slice(-99), message]);
      } catch (e) {
        console.error('WS parse error:', e);
      }
    };

    ws.onclose = () => setIsConnected(false);
    ws.onerror = (err) => console.error('WS error:', err);

    setSocket(ws);

    return () => ws.close();
  }, []);

  useEffect(() => {
    if (socket && isConnected) {
      subscriptions.forEach(sub => socket.send(JSON.stringify({ action: 'subscribe', type: sub })));
    }
  }, [subscriptions, socket, isConnected]);

  const subscribe = useCallback((type: string) => {
    setSubscriptions(prev => new Set([...prev, type]));
    if (socket && isConnected) {
      socket.send(JSON.stringify({ action: 'subscribe', type }));
    }
  }, [socket, isConnected]);

  const unsubscribe = useCallback((type: string) => {
    setSubscriptions(prev => {
      const next = new Set(prev);
      next.delete(type);
      return next;
    });
    if (socket && isConnected) {
      socket.send(JSON.stringify({ action: 'unsubscribe', type }));
    }
  }, [socket, isConnected]);

  return (
    <WebSocketContext.Provider value={{ isConnected, lastMessage, messages, subscribe, unsubscribe }}>
      {children}
    </WebSocketContext.Provider>
  );
};

export const useWebSocket = () => {
  const context = useContext(WebSocketContext);
  if (!context) throw new Error('useWebSocket must be used within WebSocketProvider');
  return context;
};

export const useRealtimeStats = () => {
  const { lastMessage } = useWebSocket();
  return lastMessage?.type === 'stats' ? lastMessage.payload : null;
};

export const useRealtimeQueries = () => {
  const { messages } = useWebSocket();
  return messages.filter(m => m.type === 'query').slice(-50);
};
