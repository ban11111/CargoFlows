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

    var body: some View {
        NavigationStack {
            List(filteredSKUs) { sku in
                NavigationLink(value: sku.id) {
                    SKUListRow(sku: sku)
                }
            }
            .navigationTitle("SKU")
            .searchable(text: $searchText, prompt: language.t("sku.search"))
            .navigationDestination(for: Int.self) { id in
                SKUDetailView(skuID: id)
            }
            .overlay {
                if isLoading {
                    ProgressView()
                } else if let errorMessage {
                    ContentUnavailableView(language.t("load.failed"), systemImage: "wifi.exclamationmark", description: Text(errorMessage))
                }
            }
            .task {
                await load()
            }
            .refreshable {
                await load()
            }
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

private struct SKUListRow: View {
    @EnvironmentObject private var language: LanguageStore
    let sku: SKU

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(sku.code)
                    .font(.headline)
                Spacer()
                if sku.isLowStock {
                    Label(language.t("low.stock"), systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(.red)
                }
            }
            Text(sku.product.name)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            if !sku.tags.isEmpty {
                Text(sku.tags.map(\.name).joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.tint)
                    .lineLimit(1)
            }
            HStack {
                Text("\(sku.color) / \(sku.size)")
                Spacer()
                Text(String(format: language.t("stock.count"), "\(sku.stock)"))
                    .fontWeight(sku.isLowStock ? .semibold : .regular)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }
}
