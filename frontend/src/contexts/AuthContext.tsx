import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import axios from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:9090/api/v1';

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('auth_token');
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);

interface User {
  id: string;
  username: string;
  roles: string[];
}

interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (username: string, password: string) => Promise<boolean>;
  logout: () => void;
  hasPermission: (permission: string) => boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const token = localStorage.getItem('auth_token');
    const storedUser = localStorage.getItem('auth_user');
    if (token && storedUser) {
      try {
        setUser(JSON.parse(storedUser));
      } catch {
        localStorage.removeItem('auth_token');
        localStorage.removeItem('auth_user');
      }
    }
    setIsLoading(false);
  }, []);

  const login = async (username: string, _password: string): Promise<boolean> => {
    // Demo mode: accept admin/admin
    if (username === 'admin' && _password === 'admin') {
      const demoUser: User = { id: '1', username: 'admin', roles: ['admin'] };
      localStorage.setItem('auth_token', 'demo-token');
      localStorage.setItem('auth_user', JSON.stringify(demoUser));
      setUser(demoUser);
      return true;
    }
    return false;
  };

  const logout = () => {
    localStorage.removeItem('auth_token');
    localStorage.removeItem('auth_user');
    setUser(null);
  };

  const hasPermission = (permission: string) => {
    if (!user) return false;
    const rolePermissions: Record<string, string[]> = {
      admin: ['*'],
      operator: ['read', 'execute', 'metrics'],
      developer: ['read', 'write', 'execute', 'config'],
      auditor: ['read', 'audit'],
    };
    const permissions = rolePermissions[user.roles[0]] || [];
    return permissions.includes('*') || permissions.includes(permission);
  };

  return (
    <AuthContext.Provider value={{ user, isAuthenticated: !!user, isLoading, login, logout, hasPermission }}>
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used within AuthProvider');
  return context;
};

export const ProtectedRoute: React.FC<{ children: ReactNode; permission?: string }> = ({ children, permission }) => {
  const { isAuthenticated, isLoading, hasPermission } = useAuth();

  if (isLoading) return <div>Loading...</div>;
  if (!isAuthenticated) return <div>Please login</div>;
  if (permission && !hasPermission(permission)) return <div>Access denied</div>;

  return <>{children}</>;
};

export default api;
