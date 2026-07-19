import SwiftUI

struct SKUDetailView: View {
    let skuID: String

    @EnvironmentObject private var language: LanguageStore
    @State private var sku: SKU?
    @State private var isLoading = false
    @State private var loadFailed = false
    @State private var isAdjustmentPresented = false
    @State private var isCapturePresented = false

    var body: some View {
        ScrollView {
            if let sku {
                VStack(spacing: 16) {
                    hero(sku)
                    stockPanel(sku)
                    productPanel(sku)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
            } else if loadFailed {
                ContentUnavailableView {
                    Label(language.t("load.failed"), systemImage: "wifi.exclamationmark")
                } description: {
                    Text(language.t("api.unreachable"))
                } actions: {
                    Button(language.t("retry")) { Task { await load() } }
                }
                .frame(minHeight: 440)
            }
        }
        .background(CargoTheme.canvas)
        .navigationTitle(sku?.code ?? language.t("sku.section"))
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if isLoading && sku == nil { ProgressView() }
        }
        .safeAreaInset(edge: .bottom) {
            if sku != nil {
                actionBar
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .padding(.bottom, 6)
                    .background(.bar)
            }
        }
        .task { await load() }
        .sheet(isPresented: $isAdjustmentPresented) {
            if let sku {
                InventoryAdjustmentView(sku: sku) { await load() }
            }
        }
        .sheet(isPresented: $isCapturePresented) {
            if let sku { SOPCaptureView(sku: sku) }
        }
    }

    private func hero(_ sku: SKU) -> some View {
        CargoPanel {
            VStack(alignment: .leading, spacing: 16) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 5) {
                        Text(sku.code)
                            .font(.title2.weight(.bold))
                        Text(sku.product.name)
                            .font(.body)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    CargoStatusPill(
                        title: sku.isLowStock ? language.t("low.stock") : language.t("inventory.available"),
                        systemImage: sku.isLowStock ? "exclamationmark.triangle.fill" : "checkmark.circle.fill",
                        tint: sku.isLowStock ? .orange : .green
                    )
                }
                Divider()
                Label("\(sku.color) · \(sku.size)", systemImage: "shippingbox")
                    .font(.subheadline.weight(.medium))
            }
        }
    }

    private func stockPanel(_ sku: SKU) -> some View {
        CargoPanel {
            VStack(alignment: .leading, spacing: 16) {
                Label(language.t("stock"), systemImage: "chart.bar.fill")
                    .font(.headline)
                HStack(spacing: 0) {
                    CargoMetric(value: "\(sku.stock)", label: language.t("current.stock"), tint: sku.isLowStock ? .orange : CargoTheme.accentDeep)
                    Divider().frame(height: 44)
                    CargoMetric(value: "\(sku.lowStockThreshold)", label: language.t("threshold"))
                        .padding(.leading, 18)
                }
            }
        }
    }

    private func productPanel(_ sku: SKU) -> some View {
        CargoPanel {
            VStack(alignment: .leading, spacing: 16) {
                Label(language.t("product.info"), systemImage: "doc.text")
                    .font(.headline)
                Divider()
                DetailRow(label: language.t("brand"), value: sku.product.brand)
                DetailRow(label: language.t("category"), value: sku.product.categoryDisplayName(for: language.language))
                DetailRow(label: language.t("platform.title"), value: sku.platformTitle)
                if !sku.tags.isEmpty {
                    DetailRow(label: language.t("sku.tags"), value: sku.tags.map(\.name).joined(separator: " · "))
                }
            }
        }
    }

    private var actionBar: some View {
        HStack(spacing: 10) {
            Button { isAdjustmentPresented = true } label: {
                Label(language.t("adjust.inventory"), systemImage: "plusminus")
            }
            .buttonStyle(CargoSecondaryButtonStyle())
            Button { isCapturePresented = true } label: {
                Label(language.t("start.sop.capture"), systemImage: "camera.viewfinder")
            }
            .buttonStyle(CargoPrimaryButtonStyle())
        }
    }

    private func load() async {
        isLoading = true
        loadFailed = false
        do {
            sku = try await APIClient.shared.getSKU(id: skuID)
        } catch {
            loadFailed = true
        }
        isLoading = false
    }
}

private struct DetailRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 16) {
            Text(label).foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .multilineTextAlignment(.trailing)
                .fontWeight(.medium)
        }
        .font(.subheadline)
        .accessibilityElement(children: .combine)
    }
}
