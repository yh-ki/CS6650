from locust import HttpUser, task, between
import json
import random

class SyncOrderUser(HttpUser):
    wait_time = between(0.1, 0.5)

    @task
    def place_sync_order(self):
        payload = {
            "customer_id": random.randint(1, 1000),
            "items": [
                {
                    "product_id": f"item-{random.randint(1, 50)}",
                    "quantity": random.randint(1, 5),
                    "price": round(random.uniform(5.0, 100.0), 2)
                }
            ]
        }
        with self.client.post(
            "/orders/sync",
            json=payload,
            catch_response=True
        ) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Got {response.status_code}")


class AsyncOrderUser(HttpUser):
    wait_time = between(0.1, 0.5)

    @task
    def place_async_order(self):
        payload = {
            "customer_id": random.randint(1, 1000),
            "items": [
                {
                    "product_id": f"item-{random.randint(1, 50)}",
                    "quantity": random.randint(1, 5),
                    "price": round(random.uniform(5.0, 100.0), 2)
                }
            ]
        }
        with self.client.post(
            "/orders/async",
            json=payload,
            catch_response=True
        ) as response:
            if response.status_code == 202:
                response.success()
            else:
                response.failure(f"Got {response.status_code}")
