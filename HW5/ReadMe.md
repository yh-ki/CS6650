## Project Structure
```
HW5
├── ReadMe.md
├── locustfile.py
├── src
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── terraform.tfstate
├── terraform
   ├── main.tf
   ├── modules
   │   ├── ecr
   │   ├── ecs
   │   ├── logging
   │   └── network
   ├── outputs.tf
   ├── provider.tf
   ├── terraform.tfstate
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

### Get repository_url
```
terraform state show module.ecr.aws_ecr_repository.this | grep repository_url
```
### Testing Deployment
```bash
# Create a product
curl -X POST http://<repository_url>:8080/products/1/details \
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
curl http://<repository_url>:8080/products/1
```

### Load Testing

```bash
locust -f tests/locustfile.py --host=http://<repository_url>
```
