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
    @State private var isSaving = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(sku.code).font(.headline)
                            Text(sku.product.name).font(.subheadline).foregroundStyle(.secondary)
                        }
                        Spacer()
                        CargoMetric(value: "\(sku.stock)", label: language.t("current.stock"))
                            .frame(maxWidth: 90)
                    }
                    .padding(.vertical, 6)
                }

                Section(language.t("inventory.adjustment")) {
                    TextField(language.t("quantity.placeholder"), text: $quantityDelta)
                        .cargoNumberKeyboard()
                    TextField(language.t("reason"), text: $reason)
                    TextField(language.t("note"), text: $note, axis: .vertical)
                        .lineLimit(2...5)
                }
                if let errorMessage {
                    Section {
                        Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle(String(format: language.t("adjust.title"), sku.code))
            .overlay {
                if isSaving {
                    ProgressView()
                        .padding(18)
                        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(language.t("cancel")) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(language.t("save")) { Task { await save() } }
                        .disabled(isSaving || quantityDelta.isEmpty || reason.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
    }

    private func save() async {
        guard let delta = Int(quantityDelta), delta != 0, !reason.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            errorMessage = language.t("adjust.validation")
            return
        }
        isSaving = true
        defer { isSaving = false }
        do {
            _ = try await APIClient.shared.adjustInventory(skuID: sku.id, quantityDelta: delta, reason: reason, note: note.isEmpty ? nil : note)
            await onSaved()
            dismiss()
        } catch {
            errorMessage = language.t("save.failed")
        }
    }
}
