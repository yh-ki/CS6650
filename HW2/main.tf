# You probably want to keep your ip address a secret as well
variable "ssh_cidr" {
  type        = string
  description = "Your home IP in CIDR notation"
}

# name of the existing AWS key pair
variable "ssh_key_name" {
  type        = string
  description = "Name of your existing AWS key pair"
}

# Number of instances to create
variable "instance_count" {
  type        = number
  description = "Number of EC2 instances to create"
  default     = 2
}

# The provider of your cloud service, in this case it is AWS. 
provider "aws" {
  region = "us-west-2" # Which region you are working on
}

# Your ec2 instances (creates multiple using count)
resource "aws_instance" "demo-instance" {
  count                  = var.instance_count
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t2.micro"
  iam_instance_profile   = "LabInstanceProfile"
  vpc_security_group_ids = [aws_security_group.ssh.id]
  key_name               = var.ssh_key_name

  tags = {
    Name = "terraform-created-instance-${count.index + 1}"
  }
}

# Your security group that grants ssh access from 
# your ip address to your ec2 instance
resource "aws_security_group" "ssh" {
  name        = "allow_ssh_from_me"
  description = "SSH from a single IP"
  
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.ssh_cidr]
  }

  ingress {
    description = "HTTP on port 8080"
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# latest Amazon Linux 2023 AMI
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64-ebs"]
  }
}

# Outputs for all instances
output "instance_public_ips" {
  value       = aws_instance.demo-instance[*].public_ip
  description = "Public IPs of all instances"
}

output "instance_public_dns" {
  value       = aws_instance.demo-instance[*].public_dns
  description = "Public DNS names of all instances"
}

output "instance_details" {
  value = {
    for idx, instance in aws_instance.demo-instance :
    "instance-${idx + 1}" => {
      public_ip  = instance.public_ip
      public_dns = instance.public_dns
    }
  }
  description = "Detailed information about all instances"
}