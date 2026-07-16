import SwiftUI

struct InventoryAdjustmentView: View {
    let sku: SKU
    let onSaved: () async -> Void

    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var language: LanguageStore
    @State private var quantityDelta = ""
    @State private var reason = ""
    @State private var note = ""
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                Section(language.t("inventory.adjustment")) {
                    TextField(language.t("quantity.placeholder"), text: $quantityDelta)
                        .cargoNumberKeyboard()
                    TextField(language.t("reason"), text: $reason)
                    TextField(language.t("note"), text: $note, axis: .vertical)
                }
                if let errorMessage {
                    Section {
                        Text(errorMessage)
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle(String(format: language.t("adjust.title"), sku.code))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(language.t("cancel")) {
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(language.t("save")) {
                        Task { await save() }
                    }
                }
            }
        }
    }

    private func save() async {
        guard let delta = Int(quantityDelta), !reason.isEmpty else {
            errorMessage = language.t("adjust.validation")
            return
        }
        do {
            _ = try await APIClient.shared.adjustInventory(skuID: sku.id, quantityDelta: delta, reason: reason, note: note.isEmpty ? nil : note)
            await onSaved()
            dismiss()
        } catch {
            errorMessage = language.t("save.failed")
        }
    }
}
