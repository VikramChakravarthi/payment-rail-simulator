package main

import "time"

// this is the top level type
// it means that incoming JSON should look like below
/*	{
  	  "FIToFICstmrCdtTrf": {
    	...
	}
}			*/
type Document struct {
	FIToFICstmrCdtTrf FIToFICustomerCreditTransfer `json:"FIToFICstmrCdtTrf"`
}

// GrpHdr is the group/message-level information (message ID, creation time, number of transactions, 
// and settler method). 
// CdtTrfTxInf is actual credit transfer/payment details (payment IDs, amount, debtor, creditor, 
// accounts, banks, and remmitance info)
type FIToFICustomerCreditTransfer struct {
	GrpHdr      GroupHeader               `json:"GrpHdr"`
	CdtTrfTxInf CreditTransferTransaction `json:"CdtTrfTxInf"`
}

type GroupHeader struct {
	MsgId    string                `json:"MsgId"`
	CreDtTm  time.Time             `json:"CreDtTm"`
	NbOfTxs  string                `json:"NbOfTxs"`
	SttlmInf SettlementInstruction `json:"SttlmInf"`
}

type SettlementInstruction struct {
	SttlmMtd string `json:"SttlmMtd"`
}

type CreditTransferTransaction struct {
	PmtId          PaymentIdentification `json:"PmtId"`
	IntrBkSttlmAmt Amount                `json:"IntrBkSttlmAmt"`
	ChrgBr         string                `json:"ChrgBr"`
	Dbtr           Party                 `json:"Dbtr"`
	DbtrAcct       Account               `json:"DbtrAcct"`
	DbtrAgt        Agent                 `json:"DbtrAgt"`
	CdtrAgt        Agent                 `json:"CdtrAgt"`
	Cdtr           Party                 `json:"Cdtr"`
	CdtrAcct       Account               `json:"CdtrAcct"`
	RmtInf         RemittanceInfo        `json:"RmtInf,omitempty"`
}

type PaymentIdentification struct {
	InstrId    string `json:"InstrId,omitempty"`
	EndToEndId string `json:"EndToEndId"`
	TxId       string `json:"TxId,omitempty"`
	UETR       string `json:"UETR"`
}

type Amount struct {
	Ccy   string  `json:"Ccy"`
	Value float64 `json:"value"`
}

type Party struct {
	Nm string `json:"Nm"`
}

type Account struct {
	Id AccountId `json:"Id"`
}

type AccountId struct {
	Othr GenericId `json:"Othr"`
}

type GenericId struct {
	Id string `json:"Id"`
}

type Agent struct {
	FinInstnId FinancialInstitution `json:"FinInstnId"`
}

type FinancialInstitution struct {
	ClrSysMmbId ClearingSystemMember `json:"ClrSysMmbId"`
}

type ClearingSystemMember struct {
	MmbId string `json:"MmbId"`
}

type RemittanceInfo struct {
	Ustrd string `json:"Ustrd,omitempty"`
}
