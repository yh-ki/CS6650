import requests

# Correct IP addresses for both instances
EC2_URL1 = "http://35.88.233.196:8080/albums"
EC2_URL2 = "http://35.162.156.231:8080/albums"

POST_DATA = {
    "id": "4",
    "title": "The Modern Sound of Betty Carter",
    "artist": "Betty Carter",
    "price": 49.99
}
HEADERS = {
    "Content-Type": "application/json"
}


def data_from_both(url1, url2):
    try:
        response = requests.get(url1)
        print(f"Instance 1 response: {response.text}")
    except requests.exceptions.RequestException as e:
        print(f"Error occurred while testing {url1}: {e}")

    print("\n\nand...")
    try:
        response = requests.get(url2)
        print(f"Instance 2 response: {response.text}")
    except requests.exceptions.RequestException as e:
        print(f"Error occurred while testing {url2}: {e}")
    return


print("Starting data test...")

data_from_both(EC2_URL1, EC2_URL2)

print("\n\nand adding...")
try:
    response = requests.post(EC2_URL2, json=POST_DATA, headers=HEADERS)
    print(f"Instance 2 response: {response.text}")
except requests.exceptions.RequestException as e:
    print(f"Error occurred while testing {EC2_URL2}: {e}")

data_from_both(EC2_URL1, EC2_URL2)

print("uhoh... what happened?")