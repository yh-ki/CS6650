from locust import HttpUser, task, between
from locust import FastHttpUser  # For the FastHttpUser experiment
import random


class AlbumUser(FastHttpUser):
    """
    Standard HttpUser for testing the albums API.
    Change to FastHttpUser for the performance comparison experiment.
    """
    
    # Wait between 1-2 seconds between tasks to simulate real user behavior
    # Comment this out for high-throughput tests
    wait_time = between(1, 2)
    
    def on_start(self):
        """Called when a simulated user starts"""
        # You could add authentication or setup here if needed
        pass
    
    @task(3)  # Weight of 3 - will run 3x more often than POST
    def get_all_albums(self):
        """GET request to fetch all albums"""
        self.client.get("/albums")
    
    @task(1)  # Weight of 1 - creates 3:1 ratio of GET:POST
    def post_album(self):
        """POST request to create a new album"""
        # Generate random album data
        new_album = {
            "id": str(random.randint(100, 9999)),
            "title": f"Test Album {random.randint(1, 1000)}",
            "artist": f"Test Artist {random.randint(1, 100)}",
            "price": round(random.uniform(9.99, 99.99), 2)
        }
        
        self.client.post(
            "/albums",
            json=new_album,
            headers={"Content-Type": "application/json"}
        )


class AlbumFastHttpUser(FastHttpUser):
    """
    FastHttpUser version for performance comparison.
    This uses a C-based HTTP client for higher throughput.
    
    To use this: Change 'locustfile.py' to use this class instead,
    or create a separate file like 'locustfile_fast.py'
    """
    
    # For high-throughput tests, remove or reduce wait_time
    # wait_time = between(1, 2)
    
    @task(3)
    def get_all_albums(self):
        """GET request to fetch all albums"""
        self.client.get("/albums")
    
    @task(1)
    def post_album(self):
        """POST request to create a new album"""
        new_album = {
            "id": str(random.randint(100, 9999)),
            "title": f"Test Album {random.randint(1, 1000)}",
            "artist": f"Test Artist {random.randint(1, 100)}",
            "price": round(random.uniform(9.99, 99.99), 2)
        }
        
        self.client.post(
            "/albums",
            json=new_album,
            headers={"Content-Type": "application/json"}
        )


# EXPERIMENT CONFIGURATIONS
# =========================

# Experiment 1: Basic Test (1 worker, 1 user)
# - Verify no failures
# - Understand baseline performance
# - Compare GET vs POST latencies

# Experiment 2: Load Test (1 worker, 50 users, 10/sec ramp)
# - GET:POST ratio = 3:1
# - Monitor CPU usage with `docker stats`
# - Record throughput and response times

# Experiment 3: Amdahl's Law (4 workers, 50 users)
# - Compare throughput scaling
# - Does throughput scale linearly with workers?
# - Consider read/write contention on shared data

# Experiment 4: FastHttpUser vs HttpUser
# - Switch to FastHttpUser
# - Compare throughput and latency
# - Understand context switching overhead