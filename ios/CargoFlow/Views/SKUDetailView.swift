import SwiftUI

struct SKUDetailView: View {
    let skuID: Int

    @EnvironmentObject private var language: LanguageStore
    @State private var sku: SKU?
    @State private var isAdjustmentPresented = false
    @State private var isCapturePresented = false

    var body: some View {
        List {
            if let sku {
                Section(language.t("product.info")) {
                    LabeledContent(language.t("product"), value: sku.product.name)
                    LabeledContent(language.t("brand"), value: sku.product.brand)
                    LabeledContent(language.t("category"), value: sku.product.categoryDisplayName(for: language.language))
                    LabeledContent(language.t("spec"), value: "\(sku.color) / \(sku.size)")
                    LabeledContent(language.t("platform.title"), value: sku.platformTitle)
                    if !sku.tags.isEmpty {
                        LabeledContent(language.t("sku.tags"), value: sku.tags.map(\.name).joined(separator: " · "))
                    }
                }

                Section(language.t("stock")) {
                    LabeledContent(language.t("current.stock"), value: "\(sku.stock)")
                    LabeledContent(language.t("threshold"), value: "\(sku.lowStockThreshold)")
                    Button(language.t("adjust.inventory")) {
                        isAdjustmentPresented = true
                    }
                }

                Section(language.t("capture")) {
                    Button(language.t("start.sop.capture")) {
                        isCapturePresented = true
                    }
                }
            } else {
                ProgressView()
            }
        }
        .navigationTitle(sku?.code ?? "SKU")
        .task {
            await load()
        }
        .sheet(isPresented: $isAdjustmentPresented) {
            if let sku {
                InventoryAdjustmentView(sku: sku) {
                    await load()
                }
            }
        }
        .sheet(isPresented: $isCapturePresented) {
            if let sku {
                SOPCaptureView(sku: sku)
            }
        }
    }

    private func load() async {
        sku = try? await APIClient.shared.getSKU(id: skuID)
    }
}
