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

struct Product: Decodable {
    let categoryID: Int
    let name: String
    let brand: String
    let category: String
    let description: String?
    let catalogCategory: CatalogCategory?

    enum CodingKeys: String, CodingKey {
        case name, brand, category, description
        case categoryID = "category_id"
        case catalogCategory = "category_record"
    }

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
        case nameEN = "name_en"
    }

    func displayName(for language: AppLanguage) -> String {
        language == .en && !nameEN.isEmpty ? nameEN : name
    }
}

struct SKUTag: Identifiable, Decodable {
    let name: String
    var id: String { name }
}

struct SKU: Identifiable, Decodable {
    let publicID: String
    var id: String { publicID }
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
    let tags: [SKUTag]

    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case code, color, size, barcode, stock
        case lowStockThreshold = "low_stock_threshold"
        case platformTitle = "platform_title"
        case sellingPoints = "selling_points"
        case status, product, tags
    }

    var isLowStock: Bool {
        stock <= lowStockThreshold
    }
}

struct InventoryAdjustment: Identifiable, Decodable {
    let skuID: String
    let quantityDelta: Int
    let reason: String
    let note: String?
    let operatorName: String
    let createdAt: Date
    var id: String { "\(skuID)-\(createdAt.timeIntervalSince1970)" }

    enum CodingKeys: String, CodingKey {
        case skuID = "sku_id"
        case quantityDelta = "quantity_delta"
        case reason, note
        case operatorName = "operator_name"
        case createdAt = "created_at"
    }
}

struct InventoryAdjustmentRequest: Encodable {
    let quantityDelta: Int
    let reason: String
    let note: String?

    enum CodingKeys: String, CodingKey {
        case quantityDelta = "quantity_delta"
        case reason, note
    }
}

struct LocalizedText: Decodable {
    let zhCN: String
    let en: String

    enum CodingKeys: String, CodingKey {
        case zhCN = "zh-CN"
        case en
    }

    func value(for language: AppLanguage) -> String {
        let preferred = language == .en ? en : zhCN
        let fallback = language == .en ? zhCN : en
        if !preferred.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return preferred
        }
        if !fallback.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return fallback
        }
        return ""
    }
}

struct Vector3DTO: Decodable, Equatable {
    let x: Double
    let y: Double
    let z: Double

    init(x: Double, y: Double, z: Double) {
        self.x = x
        self.y = y
        self.z = z
    }

    init(from decoder: Decoder) throws {
        var container = try decoder.unkeyedContainer()
        guard container.count == 3 else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Expected exactly three vector components")
        }
        let components = try [container.decode(Double.self), container.decode(Double.self), container.decode(Double.self)]
        guard components.allSatisfy(\.isFinite) else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Vector components must be finite")
        }
        x = components[0]
        y = components[1]
        z = components[2]
    }

    var values: [Double] { [x, y, z] }
}

struct SOPPose: Decodable {
    let space: String
    let cameraPositionDirection: Vector3DTO
    let imageUpDirection: Vector3DTO
    let target: Vector3DTO

    enum CodingKeys: String, CodingKey {
        case space, target
        case cameraPositionDirection = "camera_position_direction"
        case imageUpDirection = "image_up_direction"
    }
}

struct CompositionDTO: Decodable {
    let frameOccupancy: Double
    let aspectRatio: String
    let allowRotationCorrection: Bool
    let allowMirror: Bool

    enum CodingKeys: String, CodingKey {
        case frameOccupancy = "frame_occupancy"
        case aspectRatio = "aspect_ratio"
        case allowRotationCorrection = "allow_rotation_correction"
        case allowMirror = "allow_mirror"
    }
}

struct SOPReferenceImage: Identifiable, Decodable {
    let publicID: String
    var id: String { publicID }
    let objectKey: String
    let thumbnailURL: String
    let sortOrder: Int
    let caption: LocalizedText
    let createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case objectKey = "object_key"
        case thumbnailURL = "thumbnail_url"
        case sortOrder = "sort_order"
        case caption
        case createdAt = "created_at"
    }
}

struct SOPView: Identifiable, Decodable {
    let publicID: String
    var id: String { publicID }
    let sequence: Int
    let role: String
    let viewKind: String
    let presetKey: String?
    let name: LocalizedText
    let instruction: LocalizedText
    let required: Bool
    let pose: SOPPose
    let composition: CompositionDTO
    let referenceImages: [SOPReferenceImage]

    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case sequence, role, name, instruction, required, pose, composition
        case viewKind = "view_kind"
        case presetKey = "preset_key"
        case referenceImages = "reference_images"
    }

    func displayName(for language: AppLanguage) -> String { name.value(for: language) }
    func displayInstruction(for language: AppLanguage) -> String { instruction.value(for: language) }
}

struct CoordinateSystemDTO: Decodable {
    struct Axes: Decodable {
        let xPositive: String
        let yPositive: String
        let zPositive: String

        enum CodingKeys: String, CodingKey {
            case xPositive = "x_positive"
            case yPositive = "y_positive"
            case zPositive = "z_positive"
        }
    }

    let id: String
    let handedness: String
    let origin: String
    let unit: String
    let axes: Axes
}

struct SOPVersion: Identifiable, Decodable {
    let schemaVersion: String
    let publicID: String
    var id: String { publicID }
    let sopPublicID: String
    let versionNumber: Int
    let status: String
    let name: LocalizedText
    let description: LocalizedText
    let coordinateSystem: CoordinateSystemDTO
    let publishedAt: Date?
    let createdAt: Date?
    let updatedAt: Date?
    let views: [SOPView]

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case publicID = "public_id"
        case sopPublicID = "sop_public_id"
        case versionNumber = "version_number"
        case status, name, description, views
        case coordinateSystem = "coordinate_system"
        case publishedAt = "published_at"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

struct CaptureSOPSummary: Identifiable, Decodable {
    let publicID: String
    var id: String { publicID }
    let categoryID: Int
    let versions: [SOPVersion]
    let createdAt: Date?
    let updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case categoryID = "category_id"
        case versions
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

struct PhotoSessionRequest: Encodable {
    let skuID: String
    let sopVersionID: String

    enum CodingKeys: String, CodingKey {
        case skuID = "sku_id"
        case sopVersionID = "sop_version_id"
    }
}

struct PhotoSession: Identifiable, Decodable {
    let publicID: String
    var id: String { publicID }
    let code: String
    let skuID: String
    let sopVersionID: String
    let status: String
    let createdAt: Date

    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case code
        case skuID = "sku_id"
        case sopVersionID = "sop_version_id"
        case status
        case createdAt = "created_at"
    }
}

struct UploadURLRequest: Encodable {
    let fileName: String
    let contentType: String
    let photoSessionID: String
    let sopViewID: String

    enum CodingKeys: String, CodingKey {
        case fileName = "file_name"
        case contentType = "content_type"
        case photoSessionID = "photo_session_id"
        case sopViewID = "sop_view_id"
    }
}

struct UploadURLResponse: Decodable {
    let method: String
    let uploadURL: String
    let completionToken: String
    let expiresIn: Int
    let headers: [String: String]

    enum CodingKeys: String, CodingKey {
        case method
        case uploadURL = "upload_url"
        case completionToken = "completion_token"
        case expiresIn = "expires_in"
        case headers
    }
}

struct CompleteAssetRequest: Encodable {
    let photoSessionID: String
    let sopViewID: String
    let completionToken: String
    let capturedAt: Date

    enum CodingKeys: String, CodingKey {
        case photoSessionID = "photo_session_id"
        case sopViewID = "sop_view_id"
        case completionToken = "completion_token"
        case capturedAt = "captured_at"
    }
}

struct AssetReceipt: Decodable {
    let publicID: String
    let skuID: String
    let photoSessionID: String
    let sopViewID: String
    let mediaURL: String
    let reviewStatus: String
    let capturedAt: Date

    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case skuID = "sku_id"
        case photoSessionID = "photo_session_id"
        case sopViewID = "sop_view_id"
        case mediaURL = "media_url"
        case reviewStatus = "review_status"
        case capturedAt = "captured_at"
    }
}
