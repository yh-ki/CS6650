import random
from locust import task, constant
from locust.contrib.fasthttp import FastHttpUser


# Common search terms that will reliably match products in the catalog.
# "Electronics", "Books" etc. match on Category.
# "Alpha", "Beta" etc. match on Name (Brand is embedded in Name).
SEARCH_TERMS = [
    "Electronics",
    "Books",
    "Home",
    "Sports",
    "Alpha",
    "Beta",
    "Gamma",
    "Delta",
    "Product",
    "Clothing",
]


class ProductSearchUser(FastHttpUser):
    """
    Simulates a user continuously hitting the product search endpoint.

    FastHttpUser uses a single persistent connection per user (HTTP keep-alive),
    which maximises throughput and minimises per-request overhead — important
    for stress-testing a CPU-bound service.

    wait_time = constant(0) means requests fire back-to-back with no delay,
    creating maximum pressure on the server.
    """

    wait_time = constant(0)

    @task
    def search_products(self):
        """
        Picks a random search term and hits /products/search?q={term}.
        All terms are common enough to guarantee hits inside the 100-product
        bounded window, producing realistic CPU load on every request.
        """
        term = random.choice(SEARCH_TERMS)
        self.client.get(f"/products/search?q={term}", name="/products/search")