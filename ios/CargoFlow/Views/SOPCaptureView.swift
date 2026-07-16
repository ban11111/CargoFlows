import Foundation
import PhotosUI
import SwiftUI
import UIKit

struct SOPCaptureView: View {
    let sku: SKU

    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var language: LanguageStore
    @State private var templates: [SOPTemplate] = []
    @State private var capturedImages: [Int: UIImage] = [:]
    @State private var photoSession: PhotoSession?
    @State private var selectedView: SOPView?
    @State private var isSourcePickerPresented = false
    @State private var uploadingViewID: Int?
    @State private var isUploadErrorPresented = false

    private var views: [SOPView] {
        templates.first?.views?.sorted(by: { $0.sortOrder < $1.sortOrder }) ?? []
    }

    private var canFinish: Bool {
        views.filter(\.required).allSatisfy { capturedImages[$0.id] != nil }
    }

    var body: some View {
        NavigationStack {
            List {
                Section(language.t("sku.section")) {
                    LabeledContent(language.t("code"), value: sku.code)
                    LabeledContent(language.t("product"), value: sku.product.name)
                }

                Section(language.t("view.checklist")) {
                    ForEach(views) { view in
                        Button {
                            selectedView = view
                            isSourcePickerPresented = true
                        } label: {
                            CaptureViewRow(
                                view: view,
                                image: capturedImages[view.id],
                                isUploading: uploadingViewID == view.id,
                                isRequired: view.required,
                                language: language
                            )
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .navigationTitle(language.t("sop.capture"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(language.t("close")) {
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(language.t("finish")) {
                        dismiss()
                    }
                    .disabled(!canFinish)
                }
            }
            .task {
                templates = (try? await APIClient.shared.listSOPTemplates(category: sku.product.category).data) ?? []
            }
            .sheet(isPresented: $isSourcePickerPresented) {
                CaptureSourceView(view: selectedView) { image in
                    guard let selectedView else { return }
                    isSourcePickerPresented = false
                    Task {
                        await upload(image, for: selectedView)
                    }
                }
            }
            .alert(language.t("capture.upload.failed"), isPresented: $isUploadErrorPresented) {
                Button(language.t("close"), role: .cancel) {}
            } message: {
                Text(language.t("capture.upload.failed.desc"))
            }
        }
    }

    private func upload(_ image: UIImage, for view: SOPView) async {
        guard let imageData = image.jpegData(compressionQuality: 0.82) else {
            isUploadErrorPresented = true
            return
        }

        uploadingViewID = view.id
        defer { uploadingViewID = nil }

        do {
            let session = try await resolvedPhotoSession()
            let fileName = "view-\(view.id)-\(UUID().uuidString).jpg"
            _ = try await APIClient.shared.uploadImage(
                imageData,
                skuID: sku.id,
                sopViewID: view.id,
                photoSessionID: session.id,
                fileName: fileName
            )
            capturedImages[view.id] = image
        } catch {
            isUploadErrorPresented = true
        }
    }

    private func resolvedPhotoSession() async throws -> PhotoSession {
        if let photoSession {
            return photoSession
        }
        guard let templateID = templates.first?.id else {
            throw APIError.invalidResponse
        }
        let session = try await APIClient.shared.createPhotoSession(skuID: sku.id, sopTemplateID: templateID)
        photoSession = session
        return session
    }
}

private struct CaptureViewRow: View {
    let view: SOPView
    let image: UIImage?
    let isUploading: Bool
    let isRequired: Bool
    let language: LanguageStore

    var body: some View {
        HStack(spacing: 12) {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
                    .frame(width: 52, height: 52)
                    .clipShape(RoundedRectangle(cornerRadius: 8))
            } else {
                Image(systemName: "camera.viewfinder")
                    .frame(width: 52, height: 52)
                    .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
                    .foregroundStyle(.secondary)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text(view.name)
                    .foregroundStyle(.primary)
                Text(view.prompt)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            Spacer(minLength: 8)

            VStack(alignment: .trailing, spacing: 6) {
                if isUploading {
                    ProgressView()
                } else {
                    Image(systemName: image == nil ? "circle" : "checkmark.circle.fill")
                        .foregroundStyle(image == nil ? Color.secondary : Color.green)
                }
                if isRequired {
                    Text(language.t("required"))
                        .font(.caption)
                        .foregroundStyle(.red)
                }
            }
        }
        .contentShape(Rectangle())
    }
}

private struct CaptureSourceView: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var language: LanguageStore
    let view: SOPView?
    let onCaptured: (UIImage) -> Void

    @State private var selectedPhoto: PhotosPickerItem?
    @State private var isCameraPresented = false
    @State private var isReadingPhoto = false
    @State private var errorKey: String?

    var body: some View {
        let photoLibraryTitle = language.t("capture.photo.library")

        NavigationStack {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(view?.name ?? language.t("view"))
                        .font(.title2.bold())
                    Text(view?.prompt ?? "")
                        .foregroundStyle(.secondary)
                }

                VStack(spacing: 12) {
                    Button {
                        isCameraPresented = true
                    } label: {
                        Label(language.t("capture.camera"), systemImage: "camera.fill")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera))

                    PhotosPicker(selection: $selectedPhoto, matching: .images) {
                        Label(photoLibraryTitle, systemImage: "photo.on.rectangle")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.bordered)
                    .disabled(isReadingPhoto)
                }

                if !UIImagePickerController.isSourceTypeAvailable(.camera) {
                    Text(language.t("capture.camera.unavailable"))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                if isReadingPhoto {
                    HStack(spacing: 10) {
                        ProgressView()
                        Text(language.t("capture.preparing"))
                            .foregroundStyle(.secondary)
                    }
                }

                if let errorKey {
                    Text(language.t(errorKey))
                        .font(.caption)
                        .foregroundStyle(.red)
                }

                Spacer()
            }
            .padding(24)
            .navigationTitle(language.t("capture"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(language.t("cancel")) {
                        dismiss()
                    }
                }
            }
            .onChange(of: selectedPhoto) { _, item in
                guard let item else { return }
                Task {
                    await loadPhoto(item)
                }
            }
            .fullScreenCover(isPresented: $isCameraPresented) {
                CameraPicker { image in
                    isCameraPresented = false
                    guard let image else { return }
                    onCaptured(image)
                }
                .ignoresSafeArea()
            }
        }
    }

    private func loadPhoto(_ item: PhotosPickerItem) async {
        isReadingPhoto = true
        errorKey = nil
        defer { isReadingPhoto = false }

        do {
            guard let data = try await item.loadTransferable(type: Data.self), let image = UIImage(data: data) else {
                errorKey = "capture.image.invalid"
                return
            }
            onCaptured(image)
        } catch {
            errorKey = "capture.image.invalid"
        }
    }
}

private struct CameraPicker: UIViewControllerRepresentable {
    let onComplete: (UIImage?) -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(onComplete: onComplete)
    }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.cameraCaptureMode = .photo
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ uiViewController: UIImagePickerController, context: Context) {}

    final class Coordinator: NSObject, UINavigationControllerDelegate, UIImagePickerControllerDelegate {
        private let onComplete: (UIImage?) -> Void

        init(onComplete: @escaping (UIImage?) -> Void) {
            self.onComplete = onComplete
        }

        func imagePickerController(
            _ picker: UIImagePickerController,
            didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            picker.dismiss(animated: true) {
                self.onComplete(info[.originalImage] as? UIImage)
            }
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            picker.dismiss(animated: true) {
                self.onComplete(nil)
            }
        }
    }
}
