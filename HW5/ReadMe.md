## Project Structure
```
HW5
├── ReadMe.md
├── locustfile.py
├── src
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   └──  main.go
├── terraform
   ├── main.tf
   ├── modules
   │   ├── ecr
   │   ├── ecs
   │   ├── logging
   │   └── network
   ├── outputs.tf
   ├── provider.tf
   └── variables.tf

```

## Prerequisites
- Go 1.21+
- Docker
- Terraform 1.5+
- AWS CLI configured
- Locust

### Running Locally
```
cd src
go run main.go
```
### Testing Locally
```bash
# Create a product
curl -X POST http://localhost:8080/products/1/details \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": 1,
    "sku": "LAPTOP-001",
    "manufacturer": "TechCorp",
    "category_id": 5,
    "weight": 2500,
    "some_other_id": 100
  }'

# Get specific product
curl http://localhost:8080/products/1
```
## Deployment
### Deploy Infrastructure
```bash
cd terraform/
terraform init
terraform apply
```
## Access the API

### Via AWS Console
1. Go to ECS → Clusters → product-api-cluster
2. Click Tasks tab
3. Click on your running task
4. Scroll down to Network section
5. Look for Public IP

### Testing Deployment
```bash
# Create a product
curl -X POST http://<your_task_public_IP>:8080/products/1/details \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": 1,
    "sku": "LAPTOP-001",
    "manufacturer": "TechCorp",
    "category_id": 5,
    "weight": 2500,
    "some_other_id": 100
  }'

# Get specific product
curl http://<your_task_public_IP>:8080/products/1
```

### Load Testing

```bash
locust -f tests/locustfile.py --host=http://<your_task_public_IP>
```
