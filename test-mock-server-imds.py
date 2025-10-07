#!/usr/bin/env python3
"""
Mock IMDS server for testing rebalance recommendations
Run this and point NTH to http://localhost:8080
"""

import json
import time
from http.server import HTTPServer, BaseHTTPRequestHandler
from datetime import datetime, timedelta

class MockIMDSHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/latest/meta-data/rebalance-recommendation':
            # Return a mock rebalance recommendation
            notice_time = (datetime.utcnow() + timedelta(minutes=2)).isoformat() + 'Z'
            
            response = {
                "noticeTime": notice_time
            }
            
            self.send_response(200)
            self.send_header('Content-type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps(response).encode())
        else:
            # Return 404 for other paths
            self.send_response(404)
            self.end_headers()
    
    def log_message(self, format, *args):
        # Suppress default logging
        pass

if __name__ == '__main__':
    server = HTTPServer(('localhost', 8080), MockIMDSHandler)
    print("Mock IMDS server running on http://localhost:8080")
    print("Test rebalance recommendation endpoint: http://localhost:8080/latest/meta-data/rebalance-recommendation")
    print("Press Ctrl+C to stop")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        server.shutdown()
