import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Card,
  CardContent,
  TextField,
  Button,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Switch,
  FormControlLabel,
  Grid,
  Alert,
  Divider,
  IconButton,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Chip,
} from '@mui/material';
import {
  Save as SaveIcon,
  Refresh as RefreshIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
} from '@mui/icons-material';
import { configApi } from '../api/udbproxy';

interface ConfigSection {
  name: string;
  enabled: boolean;
  fields: Record<string, string | number | boolean>;
}

interface DatabaseConfig {
  id: string;
  name: string;
  type: string;
  host: string;
  port: number;
  username: string;
  enabled: boolean;
}

interface PoolConfig {
  minConnections: number;
  maxConnections: number;
  idleTimeout: number;
  maxLifetime: number;
}

function ConfigEditor() {
  const [config, setConfig] = useState<ConfigSection | null>(null);
  const [databases, setDatabases] = useState<DatabaseConfig[]>([]);
  const [poolConfig, setPoolConfig] = useState<PoolConfig>({
    minConnections: 5,
    maxConnections: 50,
    idleTimeout: 300000,
    maxLifetime: 3600000,
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    try {
      const data = await configApi.get();
      setConfig(data);
      if (data.fields) {
        if (data.fields.poolMin) setPoolConfig(prev => ({ ...prev, minConnections: Number(data.fields.poolMin) }));
        if (data.fields.poolMax) setPoolConfig(prev => ({ ...prev, maxConnections: Number(data.fields.poolMax) }));
        if (data.fields.idleTimeout) setPoolConfig(prev => ({ ...prev, idleTimeout: Number(data.fields.idleTimeout) }));
        if (data.fields.maxLifetime) setPoolConfig(prev => ({ ...prev, maxLifetime: Number(data.fields.maxLifetime) }));
      }
      const dbList = await configApi.getDatabases();
      setDatabases(dbList);
    } catch (e) {
      setDatabases([
        { id: '1', name: 'primary-mysql', type: 'mysql', host: 'localhost', port: 3306, username: 'root', enabled: true },
        { id: '2', name: 'analytics-pg', type: 'postgresql', host: 'localhost', port: 5432, username: 'admin', enabled: true },
      ]);
    }
    setLoading(false);
  };

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      await configApi.update({
        ...config,
        fields: {
          ...config?.fields,
          poolMin: poolConfig.minConnections,
          poolMax: poolConfig.maxConnections,
          idleTimeout: poolConfig.idleTimeout,
          maxLifetime: poolConfig.maxLifetime,
        },
      } as ConfigSection);
      setMessage({ type: 'success', text: 'Configuration saved successfully!' });
    } catch (e) {
      setMessage({ type: 'error', text: 'Failed to save configuration' });
    }
    setSaving(false);
  };

  const handleAddDatabase = () => {
    const newDb: DatabaseConfig = {
      id: String(Date.now()),
      name: '',
      type: 'mysql',
      host: 'localhost',
      port: 3306,
      username: '',
      enabled: true,
    };
    setDatabases([...databases, newDb]);
  };

  const handleDeleteDatabase = (id: string) => {
    setDatabases(databases.filter(db => db.id !== id));
  };

  const handleDatabaseChange = (id: string, field: keyof DatabaseConfig, value: string | number | boolean) => {
    setDatabases(databases.map(db => db.id === id ? { ...db, [field]: value } : db));
  };

  if (loading) {
    return <Typography>Loading configuration...</Typography>;
  }

  return (
    <Box>
      <Typography variant="h4" gutterBottom>Configuration Editor</Typography>
      
      {message && (
        <Alert severity={message.type} sx={{ mb: 2 }} onClose={() => setMessage(null)}>
          {message.text}
        </Alert>
      )}

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>Connection Pool Settings</Typography>
              <TextField
                fullWidth
                label="Min Connections"
                type="number"
                value={poolConfig.minConnections}
                onChange={(e) => setPoolConfig({ ...poolConfig, minConnections: Number(e.target.value) })}
                sx={{ mb: 2 }}
              />
              <TextField
                fullWidth
                label="Max Connections"
                type="number"
                value={poolConfig.maxConnections}
                onChange={(e) => setPoolConfig({ ...poolConfig, maxConnections: Number(e.target.value) })}
                sx={{ mb: 2 }}
              />
              <TextField
                fullWidth
                label="Idle Timeout (ms)"
                type="number"
                value={poolConfig.idleTimeout}
                onChange={(e) => setPoolConfig({ ...poolConfig, idleTimeout: Number(e.target.value) })}
                sx={{ mb: 2 }}
              />
              <TextField
                fullWidth
                label="Max Lifetime (ms)"
                type="number"
                value={poolConfig.maxLifetime}
                onChange={(e) => setPoolConfig({ ...poolConfig, maxLifetime: Number(e.target.value) })}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>Proxy Settings</Typography>
              <TextField
                fullWidth
                label="Proxy Port"
                type="number"
                defaultValue={3306}
                sx={{ mb: 2 }}
              />
              <TextField
                fullWidth
                label="Admin Port"
                type="number"
                defaultValue={8080}
                sx={{ mb: 2 }}
              />
              <FormControlLabel
                control={<Switch defaultChecked />}
                label="Enable Query Logging"
                sx={{ mb: 1 }}
              />
              <FormControlLabel
                control={<Switch defaultChecked />}
                label="Enable Metrics"
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12}>
          <Card>
            <CardContent>
              <Box display="flex" justifyContent="space-between" alignItems="center" mb={2}>
                <Typography variant="h6">Database Connections</Typography>
                <Button
                  variant="outlined"
                  startIcon={<AddIcon />}
                  onClick={handleAddDatabase}
                >
                  Add Database
                </Button>
              </Box>
              
              <TableContainer component={Paper} variant="outlined">
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>Name</TableCell>
                      <TableCell>Type</TableCell>
                      <TableCell>Host</TableCell>
                      <TableCell>Port</TableCell>
                      <TableCell>Username</TableCell>
                      <TableCell>Enabled</TableCell>
                      <TableCell align="right">Actions</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {databases.map((db) => (
                      <TableRow key={db.id}>
                        <TableCell>
                          <TextField
                            size="small"
                            value={db.name}
                            onChange={(e) => handleDatabaseChange(db.id, 'name', e.target.value)}
                            sx={{ minWidth: 120 }}
                          />
                        </TableCell>
                        <TableCell>
                          <FormControl size="small" sx={{ minWidth: 120 }}>
                            <Select
                              value={db.type}
                              onChange={(e) => handleDatabaseChange(db.id, 'type', e.target.value)}
                            >
                              <MenuItem value="mysql">MySQL</MenuItem>
                              <MenuItem value="postgresql">PostgreSQL</MenuItem>
                              <MenuItem value="mongodb">MongoDB</MenuItem>
                              <MenuItem value="redis">Redis</MenuItem>
                            </Select>
                          </FormControl>
                        </TableCell>
                        <TableCell>
                          <TextField
                            size="small"
                            value={db.host}
                            onChange={(e) => handleDatabaseChange(db.id, 'host', e.target.value)}
                            sx={{ minWidth: 100 }}
                          />
                        </TableCell>
                        <TableCell>
                          <TextField
                            size="small"
                            type="number"
                            value={db.port}
                            onChange={(e) => handleDatabaseChange(db.id, 'port', Number(e.target.value))}
                            sx={{ minWidth: 80 }}
                          />
                        </TableCell>
                        <TableCell>
                          <TextField
                            size="small"
                            value={db.username}
                            onChange={(e) => handleDatabaseChange(db.id, 'username', e.target.value)}
                            sx={{ minWidth: 100 }}
                          />
                        </TableCell>
                        <TableCell>
                          <Switch
                            checked={db.enabled}
                            onChange={(e) => handleDatabaseChange(db.id, 'enabled', e.target.checked)}
                            size="small"
                          />
                        </TableCell>
                        <TableCell align="right">
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => handleDeleteDatabase(db.id)}
                          >
                            <DeleteIcon />
                          </IconButton>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      <Box mt={3} display="flex" gap={2}>
        <Button
          variant="contained"
          startIcon={<SaveIcon />}
          onClick={handleSave}
          disabled={saving}
          size="large"
        >
          {saving ? 'Saving...' : 'Save Configuration'}
        </Button>
        <Button
          variant="outlined"
          startIcon={<RefreshIcon />}
          onClick={loadConfig}
          size="large"
        >
          Reset
        </Button>
      </Box>
    </Box>
  );
}

export default ConfigEditor;
