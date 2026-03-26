import React, { useState, useMemo } from 'react';
import {
  Box, Paper, Table, TableBody, TableCell, TableContainer, TableHead, TableRow,
  TextField, Select, MenuItem, FormControl, InputLabel, Chip, IconButton,
  Typography, Button, Dialog, DialogTitle, DialogContent, DialogActions,
  TablePagination
} from '@mui/material';
import {
  Download as DownloadIcon, FilterList as FilterIcon, Visibility as ViewIcon
} from '@mui/icons-material';
import axios from 'axios';

interface AuditLog {
  id: string;
  timestamp: string;
  user: string;
  database: string;
  query: string;
  result: string;
  duration: number;
  clientIP: string;
  metadata?: Record<string, any>;
}

interface AuditLogsProps {
  refreshInterval?: number;
}

export const AuditLogs: React.FC<AuditLogsProps> = ({ refreshInterval = 30000 }) => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [filterUser, setFilterUser] = useState<string>('all');
  const [filterResult, setFilterResult] = useState<string>('all');
  const [filterDatabase, setFilterDatabase] = useState<string>('all');
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(50);
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

  const fetchLogs = async () => {
    setLoading(true);
    try {
      const response = await axios.get('/api/v1/audit/logs', {
        params: { limit: 1000 }
      });
      setLogs(response.data);
    } catch (error) {
      console.error('Failed to fetch audit logs:', error);
    } finally {
      setLoading(false);
    }
  };

  React.useEffect(() => {
    fetchLogs();
    const interval = setInterval(fetchLogs, refreshInterval);
    return () => clearInterval(interval);
  }, [refreshInterval]);

  const filteredLogs = useMemo(() => {
    return logs.filter(log => {
      const matchesSearch = !searchQuery || 
        log.query.toLowerCase().includes(searchQuery.toLowerCase()) ||
        log.user.toLowerCase().includes(searchQuery.toLowerCase());
      
      const matchesUser = filterUser === 'all' || log.user === filterUser;
      const matchesResult = filterResult === 'all' || log.result === filterResult;
      const matchesDatabase = filterDatabase === 'all' || log.database === filterDatabase;
      
      return matchesSearch && matchesUser && matchesResult && matchesDatabase;
    });
  }, [logs, searchQuery, filterUser, filterResult, filterDatabase]);

  const uniqueUsers = useMemo(() => [...new Set(logs.map(l => l.user))], [logs]);
  const uniqueDatabases = useMemo(() => [...new Set(logs.map(l => l.database))], [logs]);

  const exportLogs = () => {
    const csv = [
      ['Timestamp', 'User', 'Database', 'Query', 'Result', 'Duration', 'Client IP'].join(','),
      ...filteredLogs.map(log => [
        log.timestamp, log.user, log.database, 
        `"${log.query.replace(/"/g, '""')}"`, log.result, log.duration, log.clientIP
      ].join(','))
    ].join('\n');

    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `audit-logs-${new Date().toISOString()}.csv`;
    a.click();
  };

  const getResultColor = (result: string) => {
    switch (result) {
      case 'success': return 'success';
      case 'error': return 'error';
      case 'blocked': return 'warning';
      default: return 'default';
    }
  };

  const handleChangePage = (event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (event: React.ChangeEvent<HTMLInputElement>) => {
    setRowsPerPage(parseInt(event.target.value, 10));
    setPage(0);
  };

  return (
    <Box>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={3}>
        <Typography variant="h5">Audit Logs</Typography>
        <Box display="flex" gap={2}>
          <Button variant="outlined" startIcon={<DownloadIcon />} onClick={exportLogs}>
            Export CSV
          </Button>
          <Button variant="contained" onClick={fetchLogs}>
            Refresh
          </Button>
        </Box>
      </Box>

      <Paper sx={{ p: 2, mb: 3 }}>
        <Box display="flex" gap={2} flexWrap="wrap">
          <TextField
            placeholder="Search logs..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            size="small"
            sx={{ minWidth: 250 }}
          />
          <FormControl size="small" sx={{ minWidth: 150 }}>
            <InputLabel>User</InputLabel>
            <Select
              value={filterUser}
              label="User"
              onChange={(e) => setFilterUser(e.target.value)}
            >
              <MenuItem value="all">All Users</MenuItem>
              {uniqueUsers.map(user => (
                <MenuItem key={user} value={user}>{user}</MenuItem>
              ))}
            </Select>
          </FormControl>
          <FormControl size="small" sx={{ minWidth: 150 }}>
            <InputLabel>Database</InputLabel>
            <Select
              value={filterDatabase}
              label="Database"
              onChange={(e) => setFilterDatabase(e.target.value)}
            >
              <MenuItem value="all">All Databases</MenuItem>
              {uniqueDatabases.map(db => (
                <MenuItem key={db} value={db}>{db}</MenuItem>
              ))}
            </Select>
          </FormControl>
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>Result</InputLabel>
            <Select
              value={filterResult}
              label="Result"
              onChange={(e) => setFilterResult(e.target.value)}
            >
              <MenuItem value="all">All</MenuItem>
              <MenuItem value="success">Success</MenuItem>
              <MenuItem value="error">Error</MenuItem>
              <MenuItem value="blocked">Blocked</MenuItem>
            </Select>
          </FormControl>
          <Chip label={`${filteredLogs.length} logs`} />
        </Box>
      </Paper>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Timestamp</TableCell>
              <TableCell>User</TableCell>
              <TableCell>Database</TableCell>
              <TableCell>Query (truncated)</TableCell>
              <TableCell>Result</TableCell>
              <TableCell>Duration</TableCell>
              <TableCell>Client IP</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {filteredLogs
              .slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage)
              .map((log) => (
              <TableRow key={log.id} hover>
                <TableCell>{new Date(log.timestamp).toLocaleString()}</TableCell>
                <TableCell>{log.user}</TableCell>
                <TableCell>{log.database}</TableCell>
                <TableCell sx={{ maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {log.query.substring(0, 100)}...
                </TableCell>
                <TableCell>
                  <Chip 
                    label={log.result} 
                    color={getResultColor(log.result)} 
                    size="small" 
                  />
                </TableCell>
                <TableCell>{log.duration}ms</TableCell>
                <TableCell>{log.clientIP}</TableCell>
                <TableCell>
                  <IconButton size="small" onClick={() => { setSelectedLog(log); setDetailOpen(true); }}>
                    <ViewIcon />
                  </IconButton>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <TablePagination
          rowsPerPageOptions={[10, 25, 50, 100]}
          component="div"
          count={filteredLogs.length}
          rowsPerPage={rowsPerPage}
          page={page}
          onPageChange={handleChangePage}
          onRowsPerPageChange={handleChangeRowsPerPage}
        />
      </TableContainer>

      <Dialog open={detailOpen} onClose={() => setDetailOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>Audit Log Details</DialogTitle>
        <DialogContent>
          {selectedLog && (
            <Box>
              <Typography variant="subtitle2">Query:</Typography>
              <Paper variant="outlined" sx={{ p: 2, fontFamily: 'monospace', mb: 2 }}>
                {selectedLog.query}
              </Paper>
              <Typography variant="subtitle2">Metadata:</Typography>
              <pre>{JSON.stringify(selectedLog.metadata, null, 2)}</pre>
            </Box>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDetailOpen(false)}>Close</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default AuditLogs;
