import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 30,
  duration: '20s',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<750'],
  },
};

const baseURL = __ENV.BASE_URL || 'http://localhost:8080';
const apiKey = __ENV.API_KEY || 'change-me';
const headers = { 'X-API-Key': apiKey };

export default function () {
  const stats = http.get(`${baseURL}/api/v1/stats`, { headers, tags: { endpoint: 'stats' } });
  check(stats, { 'stats status 200': (response) => response.status === 200 });

  const messages = http.get(`${baseURL}/api/v1/messages?page=1&page_size=20`, {
    headers,
    tags: { endpoint: 'messages' },
  });
  check(messages, { 'messages status 200': (response) => response.status === 200 });

  const outbox = http.get(`${baseURL}/api/v1/outbox?page=1&page_size=20`, {
    headers,
    tags: { endpoint: 'outbox' },
  });
  check(outbox, { 'outbox status 200': (response) => response.status === 200 });

  const health = http.get(`${baseURL}/health`, { tags: { endpoint: 'health' } });
  check(health, { 'health status 200': (response) => response.status === 200 });

  sleep(0.1);
}
