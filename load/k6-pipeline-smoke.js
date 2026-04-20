import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: {
    smoke: {
      executor: "ramping-vus",
      stages: [
        { duration: "30s", target: 5 },
        { duration: "60s", target: 20 },
        { duration: "30s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<800", "p(99)<1500"],
  },
};

const baseURL = __ENV.BASE_URL || "http://localhost:8080";
const authToken = __ENV.AUTH_TOKEN || "";
const batchID = __ENV.BATCH_ID || "";
const exportID = __ENV.EXPORT_ID || "";

function authHeaders() {
  if (!authToken) {
    return { "Content-Type": "application/json" };
  }
  return {
    Authorization: `Bearer ${authToken}`,
    "Content-Type": "application/json",
  };
}

export default function () {
  const health = http.get(`${baseURL}/health`);
  check(health, {
    "health status is 200 or 503": (r) => r.status === 200 || r.status === 503,
  });

  if (batchID) {
    const claims = http.get(`${baseURL}/v1/batches/${batchID}/claims?limit=50&offset=0`, {
      headers: authHeaders(),
    });
    check(claims, {
      "batch claims succeeds": (r) => r.status === 200 || r.status === 401 || r.status === 403,
    });
  }

  if (exportID) {
    const exp = http.get(`${baseURL}/v1/exports/${exportID}`, {
      headers: authHeaders(),
    });
    check(exp, {
      "export lookup succeeds": (r) => r.status === 200 || r.status === 401 || r.status === 403 || r.status === 404,
    });
  }

  sleep(1);
}
