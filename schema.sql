CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- unique internal database ID for each payment
                                                   -- ID created by postgress automatically
    uetr VARCHAR(36) UNIQUE NOT NULL, -- UNIQUE so that no two rows can have same UETR
    -- payment IDs
    end_to_end_id VARCHAR(35) NOT NULL,
    instr_id VARCHAR(35),
    tx_id VARCHAR(35),
    msg_id VARCHAR(35) NOT NULL,
    -- amount and currency
    amount NUMERIC(18,2) NOT NULL, -- numeric to store exact value (2 digits after decimal)
    currency VARCHAR(3) NOT NULL,
    -- debitor and creditor fields 
    debtor_name TEXT NOT NULL,
    debtor_account VARCHAR(34) NOT NULL,
    debtor_agent VARCHAR(35) NOT NULL,
    creditor_name TEXT NOT NULL,
    creditor_account VARCHAR(34) NOT NULL,
    creditor_agent VARCHAR(35) NOT NULL,
    -- remittance_info
    remittance_info TEXT,
    -- status
    status VARCHAR(20) NOT NULL DEFAULT 'received',
    reject_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payment_transition_log (
    id BIGSERIAL PRIMARY KEY, -- for numbering the transitions
    payment_id UUID REFERENCES payments(id), -- unique internal database ID for each payment
    sequence_number BIGINT NOT NULL, -- sequence number of the transition for a given payment
    from_state VARCHAR(20) NOT NULL, -- the state from which the payment is transitioning
    to_state VARCHAR(20) NOT NULL, -- the state to which the payment is transitioning
    event_type VARCHAR(20) NOT NULL, -- the event that triggered the transition
    reason TEXT, -- optional reason for the transition 
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- timestamp of when the transition occurred

    UNIQUE (payment_id, sequence_number) -- ensure that for a given payment, the sequence number is unique
);

CREATE TABLE IF NOT EXISTS banks (
    routing_number VARCHAR(35) PRIMARY KEY, -- unique routing number for each bank
    bank_name TEXT NOT NULL, -- name of the bank
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- status of the bank (active, inactive, etc.)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()  -- timestamp of when the bank was added
);

CREATE TABLE IF NOT EXISTS accounts (
    account_id VARCHAR(34) PRIMARY KEY,
    routing_numBer VARCHAR(35) NOT NULL,
    owner_name TEXT NOT NULL,
    balance NUMERIC(18,2) NOT NULL DEFAULT 0.00,
    reserved_balance NUMERIC(18,2) NOT NULL DEFAULT 0.00,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS outbox_events ( -- outbox acts as the reliability bridge between postgres and kafka
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_id UUID NOT NULL
        REFERENCES payments(id),

    event_type VARCHAR(100) NOT NULL,

    payload JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished
ON outbox_events (created_at)
WHERE published_at IS NULL; -- NULL means kafka has not received the event yet, will change to timestamp when kafka receives it