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