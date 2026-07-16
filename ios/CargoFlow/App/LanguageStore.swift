import Foundation

enum AppLanguage: String, CaseIterable, Identifiable {
    case zh
    case en

    var id: String { rawValue }
}

@MainActor
final class LanguageStore: ObservableObject {
    @Published var language: AppLanguage {
        didSet {
            UserDefaults.standard.set(language.rawValue, forKey: storageKey)
        }
    }

    private let storageKey = "cargo_flow_language"

    init() {
        let saved = UserDefaults.standard.string(forKey: storageKey)
        language = AppLanguage(rawValue: saved ?? "") ?? .zh
    }

    func t(_ key: String) -> String {
        strings[language]?[key] ?? strings[.zh]?[key] ?? key
    }
}

private let strings: [AppLanguage: [String: String]] = [
    .zh: [
        "tab.sku": "SKU",
        "tab.capture": "拍摄",
        "tab.settings": "设置",
        "language": "语言",
        "chinese": "中文",
        "english": "English",
        "login.section": "CargoFlow",
        "login.email": "邮箱",
        "login.password": "密码",
        "login.submit": "登录",
        "login.submitting": "登录中",
        "login.title": "后台登录",
        "login.error": "登录失败，请检查账号或后端服务。",
        "sku.search": "搜索 SKU、商品、分类",
        "sku.tags": "标签",
        "load.failed": "加载失败",
        "api.unreachable": "无法连接后端 API",
        "api.data.invalid": "后端返回的资料格式不正确",
        "api.server.error": "后端服务错误（%d）",
        "low.stock": "预警",
        "stock.count": "库存 %@",
        "product.info": "商品资料",
        "product": "商品",
        "brand": "品牌",
        "category": "分类",
        "spec": "规格",
        "platform.title": "平台标题",
        "stock": "库存",
        "current.stock": "当前库存",
        "threshold": "预警值",
        "adjust.inventory": "调整库存",
        "capture": "拍摄",
        "start.sop.capture": "开始 SOP 拍摄",
        "inventory.adjustment": "库存调整",
        "quantity.placeholder": "变动数量，例如 -3 或 20",
        "reason": "原因",
        "note": "备注",
        "adjust.title": "调整 %@",
        "cancel": "取消",
        "save": "保存",
        "adjust.validation": "请输入变动数量和原因。",
        "save.failed": "保存失败，请稍后重试。",
        "capture.empty.title": "从 SKU 详情开始拍摄",
        "capture.empty.desc": "进入 SKU 详情后选择“开始 SOP 拍摄”，App 会按视角 checklist 引导采集素材。",
        "sku.section": "SKU",
        "code": "编号",
        "view.checklist": "视角 checklist",
        "required": "必拍",
        "sop.capture": "SOP 拍摄",
        "close": "关闭",
        "finish": "完成",
        "view": "视角",
        "mock.capture": "模拟完成拍摄",
        "capture.camera": "使用相机拍摄",
        "capture.photo.library": "从相册上传",
        "capture.camera.unavailable": "当前设备无法使用相机，可从相册选择图片。",
        "capture.preparing": "正在准备图片...",
        "capture.image.invalid": "无法读取这张图片，请重新选择。",
        "capture.upload.failed": "上传失败",
        "capture.upload.failed.desc": "图片未能保存到素材库，请确认后端和 MinIO 服务正在运行后重试。",
        "api": "API",
        "account": "账号",
        "logout": "退出登录",
    ],
    .en: [
        "tab.sku": "SKU",
        "tab.capture": "Capture",
        "tab.settings": "Settings",
        "language": "Language",
        "chinese": "中文",
        "english": "English",
        "login.section": "CargoFlow",
        "login.email": "Email",
        "login.password": "Password",
        "login.submit": "Log in",
        "login.submitting": "Logging in",
        "login.title": "Admin Login",
        "login.error": "Login failed. Check your account or backend service.",
        "sku.search": "Search SKU, product, category",
        "sku.tags": "Tags",
        "load.failed": "Load failed",
        "api.unreachable": "Cannot connect to backend API",
        "api.data.invalid": "The backend returned invalid data",
        "api.server.error": "Backend service error (%d)",
        "low.stock": "Alert",
        "stock.count": "Stock %@",
        "product.info": "Product Info",
        "product": "Product",
        "brand": "Brand",
        "category": "Category",
        "spec": "Spec",
        "platform.title": "Platform Title",
        "stock": "Stock",
        "current.stock": "Current Stock",
        "threshold": "Threshold",
        "adjust.inventory": "Adjust Inventory",
        "capture": "Capture",
        "start.sop.capture": "Start SOP Capture",
        "inventory.adjustment": "Inventory Adjustment",
        "quantity.placeholder": "Quantity change, e.g. -3 or 20",
        "reason": "Reason",
        "note": "Note",
        "adjust.title": "Adjust %@",
        "cancel": "Cancel",
        "save": "Save",
        "adjust.validation": "Enter quantity change and reason.",
        "save.failed": "Save failed. Try again later.",
        "capture.empty.title": "Start from SKU Details",
        "capture.empty.desc": "Open a SKU and choose Start SOP Capture. The app will guide required photo angles.",
        "sku.section": "SKU",
        "code": "Code",
        "view.checklist": "View Checklist",
        "required": "Required",
        "sop.capture": "SOP Capture",
        "close": "Close",
        "finish": "Finish",
        "view": "View",
        "mock.capture": "Simulate Capture",
        "capture.camera": "Take Photo",
        "capture.photo.library": "Upload from Library",
        "capture.camera.unavailable": "Camera is unavailable on this device. Choose an image from your library.",
        "capture.preparing": "Preparing image...",
        "capture.image.invalid": "This image could not be read. Choose another one.",
        "capture.upload.failed": "Upload Failed",
        "capture.upload.failed.desc": "The image could not be saved to the asset library. Confirm the backend and MinIO are running, then try again.",
        "api": "API",
        "account": "Account",
        "logout": "Log out",
    ],
]
