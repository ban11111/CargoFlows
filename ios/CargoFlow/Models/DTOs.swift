import Foundation

struct LoginRequest: Encodable {
    let email: String
    let password: String
}

struct LoginResponse: Decodable {
    let token: String
    let user: User
}

struct ListResponse<T: Decodable>: Decodable {
    let data: [T]
}

struct User: Identifiable, Decodable {
    let id: Int
    let name: String
    let email: String
    let role: String
    let status: String
}

struct Product: Identifiable, Decodable {
    let id: Int
    let name: String
    let brand: String
    let category: String
    let description: String?
    let catalogCategory: CatalogCategory?

    func categoryDisplayName(for language: AppLanguage) -> String {
        catalogCategory?.displayName(for: language) ?? category
    }
}

struct CatalogCategory: Identifiable, Decodable {
    let id: Int
    let name: String
    let nameEN: String

    enum CodingKeys: String, CodingKey {
        case id, name
        case nameEN = "nameEn"
    }

    func displayName(for language: AppLanguage) -> String {
        language == .en && !nameEN.isEmpty ? nameEN : name
    }
}

struct SKUtag: Identifiable, Decodable {
    let id: Int
    let name: String
}

struct SKU: Identifiable, Decodable {
    let id: Int
    let productID: Int
    let code: String
    let color: String
    let size: String
    let barcode: String?
    let stock: Int
    let lowStockThreshold: Int
    let platformTitle: String
    let sellingPoints: String?
    let status: String
    let product: Product
    let tags: [SKUtag]

    enum CodingKeys: String, CodingKey {
        case id
        case productID = "productId"
        case code, color, size, barcode, stock
        case lowStockThreshold, platformTitle, sellingPoints
        case status, product, tags
    }

    var isLowStock: Bool {
        stock <= lowStockThreshold
    }
}

struct InventoryAdjustment: Identifiable, Decodable {
    let id: Int
    let skuID: Int
    let quantityDelta: Int
    let reason: String
    let note: String?
    let createdAt: Date

    enum CodingKeys: String, CodingKey {
        case id
        case skuID = "skuId"
        case quantityDelta
        case reason, note
        case createdAt
    }
}

struct InventoryAdjustmentRequest: Encodable {
    let quantityDelta: Int
    let reason: String
    let note: String?
}

struct SOPTemplate: Identifiable, Decodable {
    let id: Int
    let name: String
    let category: String
    let status: String
    let views: [SOPView]?
}

struct SOPView: Identifiable, Decodable {
    let id: Int
    let name: String
    let sortOrder: Int
    let required: Bool
    let prompt: String
    let exampleURL: String?
}

struct PhotoSessionRequest: Encodable {
    let skuID: Int
    let sopTemplateID: Int
}

struct PhotoSession: Identifiable, Decodable {
    let id: Int
    let code: String
    let skuID: Int
    let sopTemplateID: Int
    let status: String

    enum CodingKeys: String, CodingKey {
        case id, code
        case skuID = "skuId"
        case sopTemplateID = "sopTemplateId"
        case status
    }
}

struct UploadURLRequest: Encodable {
    let fileName: String
    let contentType: String
    let skuID: Int
    let sopViewID: Int
}

struct UploadURLResponse: Decodable {
    let method: String
    let uploadURL: String
    let assetURL: String
    let objectKey: String
    let expiresIn: Int
    let headers: [String: String]

    enum CodingKeys: String, CodingKey {
        case method
        case uploadURL = "uploadUrl"
        case assetURL = "assetUrl"
        case objectKey, expiresIn, headers
    }
}

struct CompleteAssetRequest: Encodable {
    let skuID: Int
    let photoSessionID: Int
    let sopViewID: Int
    let objectKey: String
    let originalURL: String
    let thumbnailURL: String?
    let capturedAt: Date
}

struct AssetReceipt: Decodable {
    let id: Int
}
