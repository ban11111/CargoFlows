import SwiftUI

struct SKUListView: View {
    @EnvironmentObject private var language: LanguageStore
    @State private var skus: [SKU] = []
    @State private var searchText = ""
    @State private var isLoading = false
    @State private var errorMessage: String?

    private var filteredSKUs: [SKU] {
        guard !searchText.isEmpty else { return skus }
        return skus.filter {
            $0.code.localizedCaseInsensitiveContains(searchText) ||
            $0.product.name.localizedCaseInsensitiveContains(searchText) ||
            $0.product.category.localizedCaseInsensitiveContains(searchText) ||
            $0.product.categoryDisplayName(for: language.language).localizedCaseInsensitiveContains(searchText)
        }
    }

    private var lowStockCount: Int { skus.filter(\.isLowStock).count }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(spacing: 14) {
                    CargoPanel {
                        HStack(spacing: 0) {
                            CargoMetric(value: "\(skus.count)", label: language.t("sku.total"))
                            Divider().frame(height: 44)
                            CargoMetric(
                                value: "\(lowStockCount)",
                                label: language.t("sku.low.stock"),
                                tint: lowStockCount > 0 ? .orange : CargoTheme.accentDeep
                            )
                            .padding(.leading, 18)
                        }
                    }
                    .padding(.bottom, 4)

                    if let errorMessage {
                        ContentUnavailableView {
                            Label(language.t("load.failed"), systemImage: "wifi.exclamationmark")
                        } description: {
                            Text(errorMessage)
                        } actions: {
                            Button(language.t("retry")) { Task { await load() } }
                                .buttonStyle(.borderedProminent)
                        }
                        .frame(minHeight: 320)
                    } else if !isLoading && filteredSKUs.isEmpty {
                        ContentUnavailableView(
                            language.t("sku.empty"),
                            systemImage: "shippingbox",
                            description: Text(language.t("sku.empty.desc"))
                        )
                        .frame(minHeight: 320)
                    } else {
                        ForEach(filteredSKUs) { sku in
                            NavigationLink(value: sku.id) {
                                SKUCard(sku: sku)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
            }
            .background(CargoTheme.canvas)
            .navigationTitle(language.t("sku.overview"))
            .searchable(text: $searchText, prompt: language.t("sku.search"))
            .navigationDestination(for: String.self) { id in
                SKUDetailView(skuID: id)
            }
            .overlay {
                if isLoading && skus.isEmpty {
                    ProgressView(language.t("sku.loading"))
                        .padding(20)
                        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                }
            }
            .task { await load() }
            .refreshable { await load() }
        }
    }

    private func load() async {
        isLoading = true
        errorMessage = nil
        do {
            skus = try await APIClient.shared.listSKUs().data
        } catch APIError.decoding {
            errorMessage = language.t("api.data.invalid")
        } catch APIError.server(let statusCode) {
            errorMessage = String(format: language.t("api.server.error"), statusCode)
        } catch {
            errorMessage = language.t("api.unreachable")
        }
        isLoading = false
    }
}

private struct SKUCard: View {
    @EnvironmentObject private var language: LanguageStore
    let sku: SKU

    var body: some View {
        HStack(spacing: 0) {
            RoundedRectangle(cornerRadius: 3, style: .continuous)
                .fill(sku.isLowStock ? Color.orange : CargoTheme.accent)
                .frame(width: 5)
                .padding(.vertical, 4)

            VStack(alignment: .leading, spacing: 13) {
                HStack(alignment: .top, spacing: 12) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(sku.code)
                            .font(.headline)
                            .foregroundStyle(.primary)
                        Text(sku.product.name)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                    }
                    Spacer(minLength: 8)
                    CargoStatusPill(
                        title: sku.isLowStock ? language.t("low.stock") : language.t("inventory.available"),
                        systemImage: sku.isLowStock ? "exclamationmark.triangle.fill" : "checkmark.circle.fill",
                        tint: sku.isLowStock ? .orange : .green
                    )
                }

                HStack(spacing: 10) {
                    Label("\(sku.color) · \(sku.size)", systemImage: "tag")
                    Spacer()
                    Text(String(format: language.t("stock.count"), "\(sku.stock)"))
                        .fontWeight(.semibold)
                        .monospacedDigit()
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.tertiary)
                }
                .font(.caption)
                .foregroundStyle(.secondary)

                if !sku.tags.isEmpty {
                    Text(sku.tags.map(\.name).joined(separator: "  ·  "))
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(CargoTheme.accent)
                        .lineLimit(1)
                }
            }
            .padding(.leading, 15)
        }
        .padding(16)
        .background(CargoTheme.elevatedSurface, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .stroke(CargoTheme.border, lineWidth: 1)
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityHint(language.t("sku.open.hint"))
    }
}
