import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Card,
  CardContent,
  Grid,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
} from '@mui/material';
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  BarChart,
  Bar,
  PieChart,
  Pie,
  Cell,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts';

interface TimeSeriesData {
  time: string;
  queries: number;
  latency: number;
  errors: number;
}

interface EngineData {
  name: string;
  value: number;
  color: string;
}

interface DatabaseLoadData {
  database: string;
  reads: number;
  writes: number;
}

const COLORS = ['#0088FE', '#00C49F', '#FFBB28', '#FF8042', '#8884d8', '#82ca9d'];

function Analytics() {
  const [timeRange, setTimeRange] = useState('1h');
  const [queryTrendData, setQueryTrendData] = useState<TimeSeriesData[]>([]);
  const [engineData, setEngineData] = useState<EngineData[]>([]);
  const [databaseLoadData, setDatabaseLoadData] = useState<DatabaseLoadData[]>([]);

  useEffect(() => {
    generateMockData();
  }, [timeRange]);

  const generateMockData = () => {
    const points = timeRange === '1h' ? 12 : timeRange === '24h' ? 24 : 7;
    const trendData: TimeSeriesData[] = [];
    
    for (let i = 0; i < points; i++) {
      const hour = new Date();
      hour.setHours(hour.getHours() - (points - i));
      trendData.push({
        time: hour.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
        queries: Math.floor(Math.random() * 1000) + 500,
        latency: Math.floor(Math.random() * 50) + 10,
        errors: Math.floor(Math.random() * 20),
      });
    }
    setQueryTrendData(trendData);

    setEngineData([
      { name: 'Security Engine', value: 35, color: '#FF6B6B' },
      { name: 'Cache Engine', value: 25, color: '#4ECDC4' },
      { name: 'Audit Engine', value: 20, color: '#45B7D1' },
      { name: 'Transform Engine', value: 15, color: '#96CEB4' },
      { name: 'AI Optimizer', value: 5, color: '#FFEAA7' },
    ]);

    setDatabaseLoadData([
      { database: 'Primary MySQL', reads: 4500, writes: 1200 },
      { database: 'Analytics PG', reads: 3200, writes: 400 },
      { database: 'User MongoDB', reads: 2100, writes: 1800 },
      { database: 'Cache Redis', reads: 8500, writes: 6200 },
    ]);
  };

  const totalQueries = queryTrendData.reduce((sum, d) => sum + d.queries, 0);
  const avgLatency = Math.round(queryTrendData.reduce((sum, d) => sum + d.latency, 0) / queryTrendData.length);
  const totalErrors = queryTrendData.reduce((sum, d) => sum + d.errors, 0);

  return (
    <Box>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={3}>
        <Typography variant="h4">Analytics & Charts</Typography>
        <FormControl size="small" sx={{ minWidth: 120 }}>
          <InputLabel>Time Range</InputLabel>
          <Select
            value={timeRange}
            label="Time Range"
            onChange={(e) => setTimeRange(e.target.value)}
          >
            <MenuItem value="1h">Last Hour</MenuItem>
            <MenuItem value="24h">Last 24 Hours</MenuItem>
            <MenuItem value="7d">Last 7 Days</MenuItem>
          </Select>
        </FormControl>
      </Box>

      <Grid container spacing={3}>
        <Grid item xs={12} md={4}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary">Total Queries</Typography>
              <Typography variant="h3">{totalQueries.toLocaleString()}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={4}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="secondary">Avg Latency</Typography>
              <Typography variant="h3">{avgLatency}ms</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={4}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="error">Error Rate</Typography>
              <Typography variant="h3">{totalErrors}</Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>Query Volume & Latency Trend</Typography>
              <ResponsiveContainer width="100%" height={300}>
                <AreaChart data={queryTrendData}>
                  <defs>
                    <linearGradient id="colorQueries" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#8884d8" stopOpacity={0.8}/>
                      <stop offset="95%" stopColor="#8884d8" stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" />
                  <YAxis yAxisId="left" />
                  <YAxis yAxisId="right" orientation="right" />
                  <Tooltip />
                  <Legend />
                  <Area
                    yAxisId="left"
                    type="monotone"
                    dataKey="queries"
                    stroke="#8884d8"
                    fillOpacity={1}
                    fill="url(#colorQueries)"
                    name="Queries"
                  />
                  <Line
                    yAxisId="right"
                    type="monotone"
                    dataKey="latency"
                    stroke="#82ca9d"
                    strokeWidth={2}
                    name="Latency (ms)"
                  />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>Engine Distribution</Typography>
              <ResponsiveContainer width="100%" height={300}>
                <PieChart>
                  <Pie
                    data={engineData}
                    cx="50%"
                    cy="50%"
                    innerRadius={60}
                    outerRadius={100}
                    paddingAngle={5}
                    dataKey="value"
                    label={({ name, percent }) => `${name} ${(percent * 100).toFixed(0)}%`}
                  >
                    {engineData.map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.color} />
                    ))}
                  </Pie>
                  <Tooltip />
                </PieChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>Database Load (Read/Write)</Typography>
              <ResponsiveContainer width="100%" height={300}>
                <BarChart data={databaseLoadData}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="database" />
                  <YAxis />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="reads" fill="#8884d8" name="Reads" />
                  <Bar dataKey="writes" fill="#82ca9d" name="Writes" />
                </BarChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" gutterBottom>Error Rate Over Time</Typography>
              <ResponsiveContainer width="100%" height={200}>
                <LineChart data={queryTrendData}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" />
                  <YAxis />
                  <Tooltip />
                  <Line
                    type="monotone"
                    dataKey="errors"
                    stroke="#ff7300"
                    strokeWidth={2}
                    name="Errors"
                  />
                </LineChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

export default Analytics;
