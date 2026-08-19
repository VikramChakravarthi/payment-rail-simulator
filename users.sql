INSERT INTO banks (routing_number, bank_name, status) VALUES
('987654321', 'Bank One', 'active'),
('876543210', 'Bank Two', 'active'),
('765432109', 'Bank Three', 'active'),
('654321098', 'Bank Four', 'active'),
('543210987', 'Bank Five', 'active')
ON CONFLICT (routing_number) DO NOTHING;

INSERT INTO accounts (
    account_id,
    routing_number,
    owner_name,
    balance,
    reserved_balance,
    currency,
    status
) VALUES
('12345678901234567890123456789012', '987654321', 'John Doe', 10000.00, 0.00, 'USD', 'active'),
('23456789012345678901234567890123', '876543210', 'Jane Smith', 200000.00, 0.00, 'USD', 'active'),
('34567890123456789012345678901234', '765432109', 'Alice Johnson', 3000000.00, 0.00, 'USD', 'active'),
('45678901234567890123456789012345', '654321098', 'Bob Brown', 400000.00, 0.00, 'USD', 'active'),
('56789012345678901234567890123456', '543210987', 'Charlie Davis', 500.00, 0.00, 'USD', 'active')
ON CONFLICT (account_id) DO NOTHING;