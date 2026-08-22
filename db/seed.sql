INSERT INTO accounts (
    account_id,
    routing_number,
    owner_name,
    balance,
    reserved_balance,
    currency,
    status
)
VALUES
(
    '34567890123456789012345678901234',
    '765432109',
    'Alice Johnson',
    3000000.00,
    0.00,
    'USD',
    'active'
),
(
    '56789012345678901234567890123456',
    '543210987',
    'Charlie Davis',
    500.00,
    0.00,
    'USD',
    'active'
);