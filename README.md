# Payment Rail Simulator

A Go-based payment processing simulator for FI-to-FI (Financial Institution to Financial Institution) customer credit transfers. Implements ISO 20022 XML message format validation and processing.

## Prerequisites

- Go 1.26 or higher
- PostgreSQL database
- Git

## Setup Instructions

### 1. Clone the repository
```bash
git clone https://github.com/vikramchakravarthi/payment_rail_simulator.git
cd payment_rail_simulator
```

### 2. Set up PostgreSQL database
```bash
# Create the database
createdb fednow

# Run the schema
psql -U postgres -d fednow -f schema.sql
```

### 3. Configure environment variables
```bash
# Copy the example env file
cp .env.example .env

# Edit .env with your actual database credentials
# Default format (update credentials as needed):
# DATABASE_URL = postgresql://postgres:devpass@localhost:5432/fednow
```

### 4. Install dependencies
```bash
go mod download
```

### 5. Run the server
```bash
go run .
```

The server will start on `http://localhost:8080`

## API Endpoints

### POST /payments
Accepts FI-to-FI customer credit transfer requests in ISO 20022 format.

**Example Request:**
```bash
curl -X POST http://localhost:8080/payments \
  -H "Content-Type: application/json" \
  -d @test_requests.txt
```

**Response:**
Returns payment transaction details including:
- `id`: Database record ID
- `uetr`: Unique End-to-End Transaction Reference
- `end_to_end_id`: End-to-End Identification
- `status`: "validated" or "rejected"
- `reject_reason`: Reason for rejection (if applicable)
- `created_at`: Timestamp of transaction creation

## Project Structure

- `main.go`: Server setup and payment handler
- `message.go`: ISO 20022 message structure definitions
- `validate.go`: Payment validation logic
- `schema.sql`: PostgreSQL database schema
- `test_requests.txt`: Sample payment request for testing
- `.env.example`: Environment variables template

## Validation Rules

The system validates:
- Required transaction identifiers (UETR, End-to-End ID)
- Amount must be positive
- Currency must be USD
- Debtor and creditor account IDs
- Debtor and creditor bank routing information

## Development

To test the API with sample data:
```bash
# Make sure server is running in one terminal
go run .

# In another terminal, run the test request
curl -i -X POST http://localhost:8080/payments \
  -H "Content-Type: application/json" \
  -d '{...}'
```

## Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DATABASE_URL` | PostgreSQL connection string | `postgresql://postgres:devpass@localhost:5432/fednow` |

## License

MIT
