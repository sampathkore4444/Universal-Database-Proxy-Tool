import React, { useState, useEffect, useMemo } from 'react';
import {
  Box, Paper, Table, TableBody, TableCell, TableContainer, TableHead, TableRow,
  TextField, Select, MenuItem, FormControl, InputLabel, Chip, IconButton,
  Typography, Collapse, Card, CardContent, InputAdornment, ToggleButton,
  ToggleButtonGroup, Tooltip
} from '@mui/material';
import {
  Search as SearchIcon, FilterList as FilterIcon,
  ExpandMore as ExpandIcon, ExpandLess as CollapseIcon,
  PlayArrow as RunIcon, Stop as StopIcon, Refresh as RefreshIcon
} from '@mui/icons-material';
import { useWebSocket } from '../contexts/WebSocketContext';

interface Query {
  id: string;
  query: string;
  user: string;
  database: string;
  operation: string;
  duration: number;
  status: string;
  timestamp: string;
  metadata?: Record<string, any>;
}

interface QueryInspectorProps {
  maxRows?: number;
}

type FilterType = 'all' | 'select' | 'insert' | 'update' | 'delete';
type ViewMode = 'table' | 'raw';

export const QueryInspector: React.FC<QueryInspectorProps> = ({ maxRows = 100 }) => {
  const [queries, setQueries] = useState<Query[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [filterOperation, setFilterOperation] = useState<FilterType>('all');
  const [filterStatus, setFilterStatus] = useState<string>('all');
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  const [viewMode, setViewMode] = useState<ViewMode>('table');
  const [autoRefresh, setAutoRefresh] = useState(true);
  const { messages, subscribe, unsubscribe, isConnected } = useWebSocket();

  useEffect(() => {
    subscribe('query');
    return () => unsubscribe('query');
  }, []);

  useEffect(() => {
    const queryMessages = messages.filter(m => m.type === 'query');
    if (queryMessages.length > 0) {
      const newQueries = queryMessages.map(m => m.payload as Query);
      setQueries(prev => [...newQueries, ...prev].slice(0, maxRows));
    }
  }, [messages]);

  const filteredQueries = useMemo(() => {
    return queries.filter(q => {
      const matchesSearch = !searchQuery || 
        q.query.toLowerCase().includes(searchQuery.toLowerCase()) ||
        q.user.toLowerCase().includes(searchQuery.toLowerCase()) ||
        q.database.toLowerCase().includes(searchQuery.toLowerCase());
      
      const matchesOperation = filterOperation === 'all' || 
        q.operation.toLowerCase() === filterOperation;
      
      const matchesStatus = filterStatus === 'all' || q.status === filterStatus;
      
      return matchesSearch && matchesOperation && matchesStatus;
    });
  }, [queries, searchQuery, filterOperation, filterStatus]);

  const toggleRow = (id: string) => {
    setExpandedRows(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'success': return 'success';
      case 'error': return 'error';
      case 'blocked': return 'warning';
      case 'timeout': return 'error';
      default: return 'default';
    }
  };

  const getDurationColor = (duration: number) => {
    if (duration < 100) return 'success.main';
    if (duration < 1000) return 'warning.main';
    return 'error.main';
  };

  return (
    <Box>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={3}>
        <Typography variant="h5">Live Query Inspector</Typography>
        <Box display="flex" gap={2} alignItems="center">
          <Chip 
            label={isConnected ? 'Live' : 'Disconnected'} 
            color={isConnected ? 'success' : 'error'} 
            size="small" 
          />
          <ToggleButtonGroup
            value={autoRefresh}
            exclusive
            onChange={(_, v) => v !== null && setAutoRefresh(v)}
            size="small"
          >
            <ToggleButton value={true}>Auto</ToggleButton>
            <ToggleButton value={false}>Manual</ToggleButton>
          </ToggleButtonGroup>
        </Box>
      </Box>

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Box display="flex" gap={2} flexWrap="wrap">
            <TextField
              placeholder="Search queries..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              size="small"
              sx={{ minWidth: 250 }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchIcon />
                  </InputAdornment>
                ),
              }}
            />
            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>Operation</InputLabel>
              <Select
                value={filterOperation}
                label="Operation"
                onChange={(e) => setFilterOperation(e.target.value as FilterType)}
              >
                <MenuItem value="all">All</MenuItem>
                <MenuItem value="select">SELECT</MenuItem>
                <MenuItem value="insert">INSERT</MenuItem>
                <MenuItem value="update">UPDATE</MenuItem>
                <MenuItem value="delete">DELETE</MenuItem>
              </Select>
            </FormControl>
            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>Status</InputLabel>
              <Select
                value={filterStatus}
                label="Status"
                onChange={(e) => setFilterStatus(e.target.value)}
              >
                <MenuItem value="all">All</MenuItem>
                <MenuItem value="success">Success</MenuItem>
                <MenuItem value="error">Error</MenuItem>
                <MenuItem value="blocked">Blocked</MenuItem>
                <MenuItem value="timeout">Timeout</MenuItem>
              </Select>
            </FormControl>
            <Chip label={`${filteredQueries.length} queries`} />
          </Box>
        </CardContent>
      </Card>

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell />
              <TableCell>Time</TableCell>
              <TableCell>Operation</TableCell>
              <TableCell>Database</TableCell>
              <TableCell>User</TableCell>
              <TableCell>Duration</TableCell>
              <TableCell>Status</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {filteredQueries.map((query) => (
              <React.Fragment key={query.id}>
                <TableRow 
                  hover 
                  onClick={() => toggleRow(query.id)}
                  sx={{ cursor: 'pointer' }}
                >
                  <TableCell>
                    {expandedRows.has(query.id) ? <CollapseIcon /> : <ExpandIcon />}
                  </TableCell>
                  <TableCell>{new Date(query.timestamp).toLocaleTimeString()}</TableCell>
                  <TableCell>
                    <Chip label={query.operation} size="small" />
                  </TableCell>
                  <TableCell>{query.database}</TableCell>
                  <TableCell>{query.user}</TableCell>
                  <TableCell sx={{ color: getDurationColor(query.duration) }}>
                    {query.duration}ms
                  </TableCell>
                  <TableCell>
                    <Chip 
                      label={query.status} 
                      color={getStatusColor(query.status)} 
                      size="small" 
                    />
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell colSpan={7} sx={{ py: 0 }}>
                    <Collapse in={expandedRows.has(query.id)}>
                      <Box p={2}>
                        <Typography variant="subtitle2" gutterBottom>Query:</Typography>
                        <Paper variant="outlined" sx={{ p: 2, fontFamily: 'monospace', mb: 2 }}>
                          {query.query}
                        </Paper>
                        {query.metadata && (
                          <>
                            <Typography variant="subtitle2" gutterBottom>Metadata:</Typography>
                            <pre style={{ fontSize: '0.8rem' }}>
                              {JSON.stringify(query.metadata, null, 2)}
                            </pre>
                          </>
                        )}
                      </Box>
                    </Collapse>
                  </TableCell>
                </TableRow>
              </React.Fragment>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
};

export default QueryInspector;
