import React, { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom';
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
  Container,
  Grid,
  Card,
  CardContent,
  Button,
  Switch,
  Chip,
  CircularProgress
} from '@mui/material';
import {
  Dashboard as DashboardIcon,
  Storage as DatabaseIcon,
  Psychology as EnginesIcon,
  Timeline as StatsIcon,
  Settings as SettingsIcon,
} from '@mui/icons-material';
import { engineApi, databaseApi, statsApi, healthApi } from './api/udbproxy';

// Engine types
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

// Database types
interface Database {
  id: string;
  name: string;
  type: string;
  host: string;
  port: number;
  status: string;
}

// Stats type
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

  if (loading) return <CircularProgress />;
  if (!stats) return <Typography>No stats available</Typography>;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Dashboard Overview</Typography>
      <Grid container spacing={3}>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Total Queries</Typography>
              <Typography variant="h3">{stats.totalQueries.toLocaleString()}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Active Queries</Typography>
              <Typography variant="h3">{stats.activeQueries}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Blocked Queries</Typography>
              <Typography variant="h3">{stats.blockedQueries}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Avg Latency</Typography>
              <Typography variant="h3">{stats.avgLatency}ms</Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
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

  if (loading) return <CircularProgress />;
  if (engines.length === 0) return <Typography>No engines available</Typography>;

  const enabledCount = engines.filter(e => e.enabled).length;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Smart Engines ({enabledCount}/{engines.length} Active)</Typography>
      <Grid container spacing={2}>
        {engines.map((engine) => (
          <Grid item xs={12} md={6} key={engine.id}>
            <Card>
              <CardContent>
                <Box display="flex" justifyContent="space-between" alignItems="center">
                  <Box>
                    <Typography variant="h6">{engine.name}</Typography>
                    <Typography variant="body2" color="textSecondary">{engine.description}</Typography>
                    {engine.stats && (
                      <Box mt={1}>
                        <Chip size="small" label={`${engine.stats.processed} queries`} />
                        <Chip size="small" label={`${engine.stats.avgLatency}ms`} sx={{ ml: 1 }} />
                      </Box>
                    )}
                  </Box>
                  <Switch checked={engine.enabled} />
                </Box>
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
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

  if (loading) return <CircularProgress />;
  if (databases.length === 0) return <Typography>No databases configured</Typography>;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Database Configuration</Typography>
      <Grid container spacing={2}>
        {databases.map((db) => (
          <Grid item xs={12} md={6} key={db.id}>
            <Card>
              <CardContent>
                <Box display="flex" justifyContent="space-between" alignItems="center">
                  <Box>
                    <Typography variant="h6">{db.name}</Typography>
                    <Typography variant="body2" color="textSecondary">
                      {db.type} • {db.host}:{db.port}
                    </Typography>
                  </Box>
                  <Chip 
                    label={db.status} 
                    color={db.status === 'active' ? 'success' : 'error'} 
                    size="small" 
                  />
                </Box>
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
      <Box mt={2}>
        <Button variant="contained" color="primary">Add Database</Button>
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

  if (loading) return <CircularProgress />;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Statistics</Typography>
      <Grid container spacing={3}>
        <Grid item xs={12} md={4}>
          <Card>
            <CardContent>
              <Typography variant="h6">P99 Latency</Typography>
              <Typography variant="h4">{stats?.p99Latency || 0}ms</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={4}>
          <Card>
            <CardContent>
              <Typography variant="h6">Active Connections</Typography>
              <Typography variant="h4">{stats?.activeConnections || 0}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={4}>
          <Card>
            <CardContent>
              <Typography variant="h6">Pooled Connections</Typography>
              <Typography variant="h4">{stats?.pooledConnections || 0}</Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

function App() {
  const drawerWidth = 240;

  return (
    <BrowserRouter>
      <Box sx={{ display: 'flex' }}>
        <AppBar position="fixed" sx={{ zIndex: (theme) => theme.zIndex.drawer + 1 }}>
          <Toolbar>
            <Typography variant="h6" noWrap component="div">
              UDBP - Universal Database Proxy
            </Typography>
          </Toolbar>
        </AppBar>
        <Drawer
          variant="permanent"
          sx={{
            width: drawerWidth,
            flexShrink: 0,
            '& .MuiDrawer-paper': {
              width: drawerWidth,
              boxSizing: 'border-box',
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
              <ListItem button component={Link} to="/stats">
                <ListItemIcon><StatsIcon /></ListItemIcon>
                <ListItemText primary="Statistics" />
              </ListItem>
              <ListItem button component={Link} to="/settings">
                <ListItemIcon><SettingsIcon /></ListItemIcon>
                <ListItemText primary="Settings" />
              </ListItem>
            </List>
          </Box>
        </Drawer>
        <Box component="main" sx={{ flexGrow: 1, p: 3 }}>
          <Toolbar />
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/engines" element={<Engines />} />
            <Route path="/databases" element={<Databases />} />
            <Route path="/stats" element={<Stats />} />
            <Route path="/settings" element={<Box><Typography variant="h4">Settings</Typography></Box>} />
          </Routes>
        </Box>
      </Box>
    </BrowserRouter>
  );
}

export default App;