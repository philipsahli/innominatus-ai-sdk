const express = require('express');
const { Pool } = require('pg');
const redis = require('redis');
const cors = require('cors');
const helmet = require('helmet');
require('dotenv').config();

const app = express();
const port = process.env.PORT || 3000;

// Middleware
app.use(express.json());
app.use(cors());
app.use(helmet());

// Database connection
const pool = new Pool({
  connectionString: process.env.DATABASE_URL || 'postgresql://localhost/sampledb',
});

// Redis connection
const redisClient = redis.createClient({
  url: process.env.REDIS_URL || 'redis://localhost:6379',
});

redisClient.on('error', (err) => console.error('Redis Client Error', err));

(async () => {
  await redisClient.connect();
})();

// Health check endpoint
app.get('/health', (req, res) => {
  res.json({
    status: 'healthy',
    service: 'sample-express-app',
    timestamp: new Date().toISOString(),
  });
});

// API endpoints
app.get('/api/users', async (req, res) => {
  try {
    // Check cache first
    const cached = await redisClient.get('users:all');
    if (cached) {
      return res.json(JSON.parse(cached));
    }

    // Query database
    const result = await pool.query('SELECT * FROM users');

    // Cache result
    await redisClient.setEx('users:all', 300, JSON.stringify(result.rows));

    res.json(result.rows);
  } catch (error) {
    console.error('Error fetching users:', error);
    res.status(500).json({ error: 'Internal server error' });
  }
});

app.post('/api/users', async (req, res) => {
  try {
    const { name, email } = req.body;
    const result = await pool.query(
      'INSERT INTO users (name, email) VALUES ($1, $2) RETURNING *',
      [name, email]
    );

    // Invalidate cache
    await redisClient.del('users:all');

    res.status(201).json(result.rows[0]);
  } catch (error) {
    console.error('Error creating user:', error);
    res.status(500).json({ error: 'Internal server error' });
  }
});

app.get('/api/users/:id', async (req, res) => {
  try {
    const { id } = req.params;
    const result = await pool.query('SELECT * FROM users WHERE id = $1', [id]);

    if (result.rows.length === 0) {
      return res.status(404).json({ error: 'User not found' });
    }

    res.json(result.rows[0]);
  } catch (error) {
    console.error('Error fetching user:', error);
    res.status(500).json({ error: 'Internal server error' });
  }
});

// Start server
app.listen(port, () => {
  console.log(`Server running on port ${port}`);
});

module.exports = app;