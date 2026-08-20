CREATE TABLE IF NOT EXISTS ledger_transactions ( -- tells who the financial transaction belongs to
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_id UUID NOT NULL UNIQUE
        REFERENCES payments(id),

    currency VARCHAR(3) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


CREATE TABLE IF NOT EXISTS ledger_entries ( -- gives information on which accounts changed, in what direction and by how much
    id BIGSERIAL PRIMARY KEY,

    ledger_transaction_id UUID NOT NULL
        REFERENCES ledger_transactions(id),

    account_id VARCHAR(34) NOT NULL
        REFERENCES accounts(account_id),

    entry_type VARCHAR(6) NOT NULL
        CHECK (entry_type IN ('debit', 'credit')),

    amount NUMERIC(18,2) NOT NULL
        CHECK (amount > 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);