package install

type InstallReport struct {
	Binary    BinaryReport   `json:"binary"`
	Artifacts ArtifactReport `json:"artifacts"`
}

func Install(explicitDir string, executable func() (string, error)) (InstallReport, error) {
	binary, err := bootstrapExecutable(explicitDir, executable)
	if err != nil {
		return InstallReport{}, err
	}
	update := installReceiptUpdate{
		Source: binary.Source,
		Method: binary.Method,
		CLI: installReceiptCLI{
			Path: binary.Path, Version: binary.Version, Commit: binary.Commit,
			BuildDate: binary.BuildDate, Hash: binary.Hash,
		},
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		return InstallReport{}, err
	}
	var receiptUpdate *installReceiptUpdate
	switch {
	case receipt.CLI.Path != "":
		// A different executable may repair artifacts, but cannot take ownership
		// from the exact binary already recorded in the receipt.
		if sameFilePath(receipt.CLI.Path, binary.Path) && (receipt.CLI.Hash == binary.Hash || binary.Status == "updated") {
			receiptUpdate = &update
		}
	case binary.Method == "bootstrap-copy" || isMimirExecutable(binary.Path):
		receiptUpdate = &update
	}
	artifacts, err := reconcileManagedArtifacts(true, "install", true, true, false, receiptUpdate)
	if err != nil {
		return InstallReport{}, err
	}
	return InstallReport{Binary: binary, Artifacts: artifacts}, nil
}
