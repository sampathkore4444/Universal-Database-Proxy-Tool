import axios from 'axios';

// API Base URL - configurable via environment
const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Response interceptor for error handling
api.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', error.message);
    return Promise.reject(error);
  }
);

// Types
export interface Engine {
  id: string;
  name: string;
  enabled: boolean;
  description: string;
  stats?: EngineStats;
}

export interface EngineStats {
  processed: number;
  avgLatency: number;
  errors?: number;
}

export interface Database {
  id: string;
  name: string;
  type: string;
  host: string;
  port: number;
  status: 'active' | 'inactive' | 'error';
  isReadReplica?: boolean;
}

export interface Stats {
  totalQueries: number;
  activeQueries: number;
  blockedQueries: number;
  avgLatency: number;
  p99Latency: number;
  activeConnections: number;
  pooledConnections: number;
}

export interface QueryRecord {
  id: string;
  query: string;
  user: string;
  database: string;
  timestamp: string;
  duration: number;
  status: string;
}

// Engine API
export const engineApi = {
  list: async (): Promise<Engine[]> => {
    const response = await api.get('/engines');
    return response.data;
  },

  get: async (id: string): Promise<Engine> => {
    const response = await api.get(`/engines/${id}`);
    return response.data;
  },

  enable: async (id: string): Promise<void> => {
    await api.post(`/engines/${id}/enable`);
  },

  disable: async (id: string): Promise<void> => {
    await api.post(`/engines/${id}/disable`);
  },

  getStats: async (id: string): Promise<EngineStats> => {
    const response = await api.get(`/engines/${id}/stats`);
    return response.data;
  },
};

// Database API
export const databaseApi = {
  list: async (): Promise<Database[]> => {
    const response = await api.get('/databases');
    return response.data;
  },

  get: async (id: string): Promise<Database> => {
    const response = await api.get(`/databases/${id}`);
    return response.data;
  },

  add: async (db: Omit<Database, 'id'>): Promise<Database> => {
    const response = await api.post('/databases', db);
    return response.data;
  },

  remove: async (id: string): Promise<void> => {
    await api.delete(`/databases/${id}`);
  },

  test: async (id: string): Promise<{ success: boolean; latency: number }> => {
    const response = await api.post(`/databases/${id}/test`);
    return response.data;
  },
};

// Stats API
export const statsApi = {
  get: async (): Promise<Stats> => {
    const response = await api.get('/stats');
    return response.data;
  },

  reset: async (): Promise<void> => {
    await api.post('/stats/reset');
  },

  getHistory: async (limit: number = 100): Promise<QueryRecord[]> => {
    const response = await api.get(`/query/history?limit=${limit}`);
    return response.data;
  },
};

// Health API
export const healthApi = {
  check: async (): Promise<{ status: string; uptime: number }> => {
    const response = await api.get('/health');
    return response.data;
  },
};

export default api;