// this file checks if the payment made is acceptable
// returns nil if validation successful, otherwise error
package main

import "fmt"

func validatePayment(doc Document) error {

	// recieves the whole document and pulls out actual transaction into tx
	tx := doc.FIToFICstmrCdtTrf.CdtTrfTxInf

	// reject payment if unique transaction reference UETR is missing
	if tx.PmtId.UETR == "" {
		return fmt.Errorf("missing UETR")
	}

	// end to end must exist
	if tx.PmtId.EndToEndId == "" {
		return fmt.Errorf("missing EndToEndId")
	}

	// account must be positive
	if tx.IntrBkSttlmAmt.Value <= 0 {
		return fmt.Errorf("IntrBkSttlmAmt.value must be positive")
	}

	// currency must be USD
	if tx.IntrBkSttlmAmt.Ccy != "USD" {
		return fmt.Errorf("only USD is supported")
	}

	// debtor account must exist
	if tx.DbtrAcct.Id.Othr.Id == "" {
		return fmt.Errorf("missing DbtrAcct identifier")
	}

	// creditor account must exist
	if tx.CdtrAcct.Id.Othr.Id == "" {
		return fmt.Errorf("missing CdtrAcct identifier")
	}

	// debtor bank/routing ID must exist
	if tx.DbtrAgt.FinInstnId.ClrSysMmbId.MmbId == "" {
		return fmt.Errorf("missing DbtrAgt routing identifier")
	}

	// creditor bank/routing ID must exist
	if tx.CdtrAgt.FinInstnId.ClrSysMmbId.MmbId == "" {
		return fmt.Errorf("missing CdtrAgt routing identifier")
	}
	return nil
}