from locust import HttpUser, FastHttpUser, task, between
import random
import json

class ProductUser(HttpUser):
    """
    HttpUser - uses requests library, good baseline
    Use this for initial testing
    """
    wait_time = between(1, 3)  # Wait 1-3 seconds between tasks
    
    def on_start(self):
        """Called when a user starts - create some test products"""
        # Create a few products for testing
        for i in range(5):
            product_id = random.randint(1, 100)
            self.client.post(
                f"/products/{product_id}/details",
                json={
                    "product_id": product_id,
                    "sku": f"SKU-{product_id}",
                    "manufacturer": f"Manufacturer {i}",
                    "category_id": random.randint(1, 10),
                    "weight": random.randint(100, 5000),
                    "some_other_id": random.randint(1, 1000)
                },
                name="/products/[id]/details"  # Group similar requests in stats
            )
    
    @task(5)  # Weight 5 - most common operation (read-heavy workload)
    def get_product(self):
        """Get a product by ID - simulates browsing"""
        product_id = random.randint(1, 100)
        with self.client.get(
            f"/products/{product_id}", 
            catch_response=True,
            name="/products/[id]"
        ) as response:
            if response.status_code == 404:
                # 404 is expected for non-existent products
                response.success()
    
    @task(1)  # Weight 1 - less common (write operation)
    def add_product_details(self):
        """Add/update product details - simulates inventory management"""
        product_id = random.randint(1, 100)
        self.client.post(
            f"/products/{product_id}/details",
            json={
                "product_id": product_id,
                "sku": f"SKU-{random.randint(1000, 9999)}",
                "manufacturer": random.choice([
                    "Acme Corp", 
                    "TechGear Inc", 
                    "Global Supplies", 
                    "Premier Products"
                ]),
                "category_id": random.randint(1, 20),
                "weight": random.randint(50, 10000),
                "some_other_id": random.randint(1, 5000)
            },
            name="/products/[id]/details"
        )
    
    @task(1)  # Weight 1 - health check
    def health_check(self):
        """Health check endpoint"""
        self.client.get("/health", name="/health")


class FastProductUser(FastHttpUser):
    """
    FastHttpUser - uses gevent, better for high concurrency
    Compare performance with HttpUser
    """
    wait_time = between(1, 3)
    
    def on_start(self):
        """Called when a user starts"""
        for i in range(5):
            product_id = random.randint(1, 100)
            self.client.post(
                f"/products/{product_id}/details",
                json={
                    "product_id": product_id,
                    "sku": f"SKU-{product_id}",
                    "manufacturer": f"Manufacturer {i}",
                    "category_id": random.randint(1, 10),
                    "weight": random.randint(100, 5000),
                    "some_other_id": random.randint(1, 1000)
                },
                name="/products/[id]/details"
            )
    
    @task(5)
    def get_product(self):
        product_id = random.randint(1, 100)
        with self.client.get(
            f"/products/{product_id}", 
            catch_response=True,
            name="/products/[id]"
        ) as response:
            if response.status_code == 404:
                response.success()
    
    @task(1)
    def add_product_details(self):
        product_id = random.randint(1, 100)
        self.client.post(
            f"/products/{product_id}/details",
            json={
                "product_id": product_id,
                "sku": f"SKU-{random.randint(1000, 9999)}",
                "manufacturer": random.choice([
                    "Acme Corp", 
                    "TechGear Inc", 
                    "Global Supplies", 
                    "Premier Products"
                ]),
                "category_id": random.randint(1, 20),
                "weight": random.randint(50, 10000),
                "some_other_id": random.randint(1, 5000)
            },
            name="/products/[id]/details"
        )


class EdgeCaseUser(HttpUser):
    """
    Test edge cases and error conditions
    Use this to verify proper error handling
    """
    wait_time = between(0.5, 2)
    
    @task(2)
    def get_nonexistent_product(self):
        """Test 404 response"""
        product_id = random.randint(10000, 99999)  # Unlikely to exist
        with self.client.get(
            f"/products/{product_id}",
            catch_response=True,
            name="/products/[id] - 404"
        ) as response:
            if response.status_code == 404:
                response.success()
            else:
                response.failure(f"Expected 404, got {response.status_code}")
    
    @task(1)
    def invalid_product_id(self):
        """Test 400 response - invalid ID"""
        invalid_ids = ["abc", "-1", "0", "999999999999999"]
        product_id = random.choice(invalid_ids)
        with self.client.get(
            f"/products/{product_id}",
            catch_response=True,
            name="/products/[id] - invalid ID"
        ) as response:
            if response.status_code == 400:
                response.success()
            else:
                response.failure(f"Expected 400, got {response.status_code}")
    
    @task(1)
    def invalid_product_data(self):
        """Test 400 response - missing required fields"""
        product_id = random.randint(1, 100)
        # Missing required field 'sku'
        with self.client.post(
            f"/products/{product_id}/details",
            json={
                "product_id": product_id,
                "manufacturer": "Test Corp",
                "category_id": 1,
                "weight": 100,
                "some_other_id": 1
            },
            catch_response=True,
            name="/products/[id]/details - invalid data"
        ) as response:
            if response.status_code == 400:
                response.success()
            else:
                response.failure(f"Expected 400, got {response.status_code}")


# Test scenarios - uncomment the one you want to use
# To use: locust -f locustfile.py --host=http://your-load-balancer-url

# Scenario 1: Normal mixed workload (default)
# Uses ProductUser with 5:1 read:write ratio

# Scenario 2: High concurrency test
# Command: locust -f locustfile.py --user-class FastProductUser --host=http://...

# Scenario 3: Edge case testing
# Command: locust -f locustfile.py --user-class EdgeCaseUser --host=http://...

# Scenario 4: Comparison test
# Command: locust -f locustfile.py --user-class ProductUser,FastProductUser --host=http://...
