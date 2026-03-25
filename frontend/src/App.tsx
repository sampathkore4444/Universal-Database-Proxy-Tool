import React from 'react';
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
  Chip
} from '@mui/material';
import {
  Dashboard as DashboardIcon,
  Storage as DatabaseIcon,
  Psychology as EnginesIcon,
  Timeline as StatsIcon,
  Settings as SettingsIcon,
  PlayArrow as StartIcon,
  Stop as StopIcon
} from '@mui/icons-material';

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

// Sample data
const engines: Engine[] = [
  { id: '1', name: 'Query Rewrite', enabled: true, description: 'Auto-rewrite queries for better performance', stats: { processed: 1250, avgLatency: 5 } },
  { id: '2', name: 'Federation', enabled: true, description: 'Cross-database query routing', stats: { processed: 850, avgLatency: 12 } },
  { id: '3', name: 'Encryption', enabled: true, description: 'Column-level encryption', stats: { processed: 320, avgLatency: 8 } },
  { id: '4', name: 'CDC', enabled: true, description: 'Change Data Capture', stats: { processed: 640, avgLatency: 3 } },
  { id: '5', name: 'Time-Series', enabled: false, description: 'Time-series data handling' },
  { id: '6', name: 'Graph', enabled: false, description: 'Relationship traversal queries' },
  { id: '7', name: 'Retry Intelligence', enabled: true, description: 'Smart retry logic', stats: { processed: 45, avgLatency: 25 } },
  { id: '8', name: 'Hotspot Detection', enabled: true, description: 'Identify hot data', stats: { processed: 980, avgLatency: 2 } },
  { id: '9', name: 'Query Cost Estimator', enabled: true, description: 'Predict query cost', stats: { processed: 1100, avgLatency: 1 } },
  { id: '10', name: 'Shadow Database', enabled: false, description: 'Mirror queries to QA' },
  { id: '11', name: 'Data Validation', enabled: false, description: 'Business rule validation' },
  { id: '12', name: 'Query Translation', enabled: false, description: 'Cross-dialect conversion' },
  { id: '13', name: 'Failover', enabled: true, description: 'Automatic database failover', stats: { processed: 12, avgLatency: 0 } },
  { id: '14', name: 'Query Versioning', enabled: false, description: 'Track query changes' },
  { id: '15', name: 'Batch Processing', enabled: true, description: 'Optimize bulk operations', stats: { processed: 180, avgLatency: 15 } },
  { id: '16', name: 'Data Compression', enabled: false, description: 'Transparent compression' },
  { id: '17', name: 'Load Balancer', enabled: true, description: 'Intelligent routing', stats: { processed: 2100, avgLatency: 1 } },
  { id: '18', name: 'Query History', enabled: true, description: 'Long-term storage', stats: { processed: 2500, avgLatency: 2 } },
];

const databases: Database[] = [
  { id: '1', name: 'primary', type: 'MySQL', host: 'localhost', port: 3306, status: 'active' },
  { id: '2', name: 'replica1', type: 'MySQL', host: 'localhost', port: 3307, status: 'active' },
  { id: '3', name: 'analytics', type: 'PostgreSQL', host: 'localhost', port: 5432, status: 'active' },
  { id: '4', name: 'cache', type: 'Redis', host: 'localhost', port: 6379, status: 'active' },
];

function Dashboard() {
  return (
    <Box>
      <Typography variant="h4" gutterBottom>Dashboard Overview</Typography>
      <Grid container spacing={3}>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Total Queries</Typography>
              <Typography variant="h3">10,245</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Active Engines</Typography>
              <Typography variant="h3">12</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Databases</Typography>
              <Typography variant="h3">4</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="textSecondary">Avg Latency</Typography>
              <Typography variant="h3">5ms</Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

function Engines() {
  return (
    <Box>
      <Typography variant="h4" gutterBottom>Smart Engines (31 Total)</Typography>
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
  return (
    <Box>
      <Typography variant="h4" gutterBottom>Statistics</Typography>
      <Card>
        <CardContent>
          <Typography variant="body1">Query Performance Metrics</Typography>
          <Typography variant="body2" color="textSecondary">
            Charts and analytics will be displayed here using Recharts
          </Typography>
        </CardContent>
      </Card>
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