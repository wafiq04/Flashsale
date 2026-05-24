import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  // Skenario: 50 Virtual Users (VU) menyerang bersamaan selama 10 detik
vus: 1000,
  duration: '15s',
};

export default function () {
  const url = 'http://localhost:8080/checkout';
  
  // Data payload karena endpoint kita menerima x-www-form-urlencoded
 let payload = JSON.stringify({
    product_id: 1
});

// 2. WAJIB pasang header Application/JSON agar Gin di Go tahu ini adalah JSON
let params = {
    headers: {
        'Content-Type': 'application/json',
    },
};

  // Tembak endpoint checkout!
  const res = http.post(url, payload, params);

  // Validasi: Pastikan response-nya adalah 202 Accepted
  check(res, {
    'status is 202': (r) => r.status === 202,
  });

  // Jeda sangat tipis (0.1 detik) antar klik per user untuk simulasi klik barbar
  sleep(0.1);
}