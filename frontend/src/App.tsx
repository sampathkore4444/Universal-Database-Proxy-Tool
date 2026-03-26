import React, { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Link, Navigate } from 'react-router-dom';
import {
  AppBar,
  Toolbar,
  Typography,
  Drawer,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Box,
  IconButton,
  Switch,
  CircularProgress,
} from '@mui/material';
import {
  Dashboard as DashboardIcon,
  Storage as DatabaseIcon,
  Psychology as EnginesIcon,
  Timeline as StatsIcon,
  Settings as SettingsIcon,
  Brightness4 as DarkModeIcon,
  Brightness7 as LightModeIcon,
  BarChart as AnalyticsIcon,
  Search as QueryIcon,
  History as AuditIcon,
} from '@mui/icons-material';
import { ThemeProvider, useThemeMode } from './contexts/ThemeContext';
import { AuthProvider } from './contexts/AuthContext';
import { WebSocketProvider } from './contexts/WebSocketContext';
import { engineApi, databaseApi, statsApi } from './api/udbproxy';
import QueryInspector from './pages/QueryInspector';
import AuditLogs from './pages/AuditLogs';
import ConfigEditor from './pages/ConfigEditor';
import Analytics from './pages/Analytics';

interface Engine {
  id: string;
  name: string;
  enabled: boolean;
  description: string;
  stats?: {
    processed: number;
    avgLatency: number;
  };
}

interface Database {
  id: string;
  name: string;
  type: string;
  host: string;
  port: number;
  status: string;
}

interface Stats {
  totalQueries: number;
  activeQueries: number;
  blockedQueries: number;
  avgLatency: number;
  p99Latency: number;
  activeConnections: number;
  pooledConnections: number;
}

function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    statsApi.get().then(data => {
      setStats(data);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  if (loading) return <Box display="flex" justifyContent="center" mt={4}><CircularProgress /></Box>;
  if (!stats) return <Typography>No stats available</Typography>;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Dashboard Overview</Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))', gap: 3 }}>
        <Box sx={{ p: 3, borderRadius: 2, bgcolor: 'primary.main', color: 'white' }}>
          <Typography variant="body2" sx={{ opacity: 0.8 }}>Total Queries</Typography>
          <Typography variant="h3">{stats.totalQueries.toLocaleString()}</Typography>
        </Box>
        <Box sx={{ p: 3, borderRadius: 2, bgcolor: 'secondary.main', color: 'white' }}>
          <Typography variant="body2" sx={{ opacity: 0.8 }}>Active Queries</Typography>
          <Typography variant="h3">{stats.activeQueries}</Typography>
        </Box>
        <Box sx={{ p: 3, borderRadius: 2, bgcolor: 'error.main', color: 'white' }}>
          <Typography variant="body2" sx={{ opacity: 0.8 }}>Blocked Queries</Typography>
          <Typography variant="h3">{stats.blockedQueries}</Typography>
        </Box>
        <Box sx={{ p: 3, borderRadius: 2, bgcolor: 'success.main', color: 'white' }}>
          <Typography variant="body2" sx={{ opacity: 0.8 }}>Avg Latency</Typography>
          <Typography variant="h3">{stats.avgLatency}ms</Typography>
        </Box>
      </Box>
    </Box>
  );
}

function Engines() {
  const [engines, setEngines] = useState<Engine[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    engineApi.list().then(data => {
      setEngines(data);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  if (loading) return <Box display="flex" justifyContent="center" mt={4}><CircularProgress /></Box>;
  if (engines.length === 0) return <Typography>No engines available</Typography>;

  const enabledCount = engines.filter(e => e.enabled).length;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Smart Engines ({enabledCount}/{engines.length} Active)</Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: 2 }}>
        {engines.map((engine) => (
          <Box key={engine.id} sx={{ p: 2, borderRadius: 2, border: '1px solid', borderColor: 'divider' }}>
            <Box display="flex" justifyContent="space-between" alignItems="center">
              <Box>
                <Typography variant="h6">{engine.name}</Typography>
                <Typography variant="body2" color="textSecondary">{engine.description}</Typography>
                {engine.stats && (
                  <Box mt={1} display="flex" gap={1}>
                    <Typography variant="caption">{engine.stats.processed} queries</Typography>
                    <Typography variant="caption">•</Typography>
                    <Typography variant="caption">{engine.stats.avgLatency}ms</Typography>
                  </Box>
                )}
              </Box>
              <Switch checked={engine.enabled} />
            </Box>
          </Box>
        ))}
      </Box>
    </Box>
  );
}

function Databases() {
  const [databases, setDatabases] = useState<Database[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    databaseApi.list().then(data => {
      setDatabases(data);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  if (loading) return <Box display="flex" justifyContent="center" mt={4}><CircularProgress /></Box>;
  if (databases.length === 0) return <Typography>No databases configured</Typography>;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Database Configuration</Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: 2 }}>
        {databases.map((db) => (
          <Box key={db.id} sx={{ p: 2, borderRadius: 2, border: '1px solid', borderColor: 'divider' }}>
            <Box display="flex" justifyContent="space-between" alignItems="center">
              <Box>
                <Typography variant="h6">{db.name}</Typography>
                <Typography variant="body2" color="textSecondary">
                  {db.type} • {db.host}:{db.port}
                </Typography>
              </Box>
              <Box
                sx={{
                  px: 1,
                  py: 0.5,
                  borderRadius: 1,
                  bgcolor: db.status === 'active' ? 'success.main' : 'error.main',
                  color: 'white',
                  fontSize: '0.75rem',
                }}
              >
                {db.status}
              </Box>
            </Box>
          </Box>
        ))}
      </Box>
    </Box>
  );
}

function Stats() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    statsApi.get().then(data => {
      setStats(data);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  if (loading) return <Box display="flex" justifyContent="center" mt={4}><CircularProgress /></Box>;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Statistics</Typography>
      <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 3 }}>
        <Box sx={{ p: 3, borderRadius: 2, border: '1px solid', borderColor: 'divider' }}>
          <Typography variant="h6">P99 Latency</Typography>
          <Typography variant="h4">{stats?.p99Latency || 0}ms</Typography>
        </Box>
        <Box sx={{ p: 3, borderRadius: 2, border: '1px solid', borderColor: 'divider' }}>
          <Typography variant="h6">Active Connections</Typography>
          <Typography variant="h4">{stats?.activeConnections || 0}</Typography>
        </Box>
        <Box sx={{ p: 3, borderRadius: 2, border: '1px solid', borderColor: 'divider' }}>
          <Typography variant="h6">Pooled Connections</Typography>
          <Typography variant="h4">{stats?.pooledConnections || 0}</Typography>
        </Box>
      </Box>
    </Box>
  );
}

function AppHeader() {
  const { mode, toggleTheme } = useThemeMode();

  return (
    <AppBar position="fixed" sx={{ zIndex: (theme) => theme.zIndex.drawer + 1 }}>
      <Toolbar>
        <Typography variant="h6" noWrap component="div" sx={{ flexGrow: 1 }}>
          UDBP - Universal Database Proxy
        </Typography>
        <IconButton color="inherit" onClick={toggleTheme}>
          {mode === 'dark' ? <LightModeIcon /> : <DarkModeIcon />}
        </IconButton>
      </Toolbar>
    </AppBar>
  );
}

function AppLayout() {
  const drawerWidth = 240;
  const { mode } = useThemeMode();

  return (
    <Box sx={{ display: 'flex' }}>
      <AppHeader />
      <Drawer
        variant="permanent"
        sx={{
          width: drawerWidth,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: drawerWidth,
            boxSizing: 'border-box',
            bgcolor: mode === 'dark' ? '#1e1e1e' : '#ffffff',
          },
        }}
      >
        <Toolbar />
        <Box sx={{ overflow: 'auto' }}>
          <List>
            <ListItem button component={Link} to="/">
              <ListItemIcon><DashboardIcon /></ListItemIcon>
              <ListItemText primary="Dashboard" />
            </ListItem>
            <ListItem button component={Link} to="/engines">
              <ListItemIcon><EnginesIcon /></ListItemIcon>
              <ListItemText primary="Smart Engines" />
            </ListItem>
            <ListItem button component={Link} to="/databases">
              <ListItemIcon><DatabaseIcon /></ListItemIcon>
              <ListItemText primary="Databases" />
            </ListItem>
            <ListItem button component={Link} to="/analytics">
              <ListItemIcon><AnalyticsIcon /></ListItemIcon>
              <ListItemText primary="Analytics" />
            </ListItem>
            <ListItem button component={Link} to="/query-inspector">
              <ListItemIcon><QueryIcon /></ListItemIcon>
              <ListItemText primary="Query Inspector" />
            </ListItem>
            <ListItem button component={Link} to="/audit-logs">
              <ListItemIcon><AuditIcon /></ListItemIcon>
              <ListItemText primary="Audit Logs" />
            </ListItem>
            <ListItem button component={Link} to="/config">
              <ListItemIcon><SettingsIcon /></ListItemIcon>
              <ListItemText primary="Configuration" />
            </ListItem>
          </List>
        </Box>
      </Drawer>
      <Box component="main" sx={{ flexGrow: 1, p: 3, minHeight: '100vh', bgcolor: 'background.default' }}>
        <Toolbar />
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/engines" element={<Engines />} />
          <Route path="/databases" element={<Databases />} />
          <Route path="/analytics" element={<Analytics />} />
          <Route path="/query-inspector" element={<QueryInspector />} />
          <Route path="/audit-logs" element={<AuditLogs />} />
          <Route path="/config" element={<ConfigEditor />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Box>
    </Box>
  );
}

function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <WebSocketProvider>
          <BrowserRouter>
            <AppLayout />
          </BrowserRouter>
        </WebSocketProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}

export default App;
