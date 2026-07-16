import Foundation
import PhotosUI
import SwiftUI
import UIKit

func requiredViewsComplete(views: [SOPView], capturedViewIDs: Set<String>) -> Bool {
    views.filter(\.required).allSatisfy { capturedViewIDs.contains($0.id) }
}

struct SOPVersionCandidate: Identifiable {
    let id: String
    let sopID: String
    let versionNumber: Int
    let name: LocalizedText
}

func publishedVersionCandidates(from summaries: [CaptureSOPSummary]) -> [SOPVersionCandidate] {
    summaries
        .flatMap { summary in
            summary.versions
                .filter { $0.status == "published" }
                .map {
                    SOPVersionCandidate(
                        id: $0.id,
                        sopID: summary.id,
                        versionNumber: $0.versionNumber,
                        name: $0.name
                    )
                }
        }
        .sorted {
            if $0.sopID != $1.sopID { return $0.sopID < $1.sopID }
            if $0.versionNumber != $1.versionNumber { return $0.versionNumber > $1.versionNumber }
            return $0.id < $1.id
        }
}

enum SOPVersionSelectionResult: Equatable {
    case unchanged
    case changed
    case needsConfirmation
}

enum SOPVersionAcceptance: Equatable {
    case accepted
    case stale
    case invalidResponse
}

struct SOPCandidateLoadToken: Equatable {
    fileprivate let generation: Int
}

struct SOPVersionLoadToken: Equatable {
    fileprivate let versionID: String
    fileprivate let generation: Int
}

@MainActor
final class SOPCaptureState: ObservableObject {
    @Published private(set) var candidates: [SOPVersionCandidate] = []
    @Published private(set) var selectedVersionID: String?
    @Published private(set) var version: SOPVersion?
    @Published private(set) var capturedViewIDs: Set<String> = []
    @Published private(set) var isCandidateLoading = true
    @Published private(set) var candidateLoadFailed = false
    private(set) var session: PhotoSession?

    private var loadGeneration = 0
    private var candidateLoadGeneration = 0
    private var sessionCreationVersionID: String?
    private var sessionWaiters: [CheckedContinuation<PhotoSession, Error>] = []

    func installCandidates(_ candidates: [SOPVersionCandidate]) {
        self.candidates = candidates
        guard !candidates.isEmpty else {
            applySelection(nil)
            return
        }
        if let selectedVersionID, candidates.contains(where: { $0.id == selectedVersionID }) {
            return
        }
        applySelection(candidates[0].id)
    }

    func requestSelection(_ versionID: String) -> SOPVersionSelectionResult {
        guard candidates.contains(where: { $0.id == versionID }) else { return .unchanged }
        guard selectedVersionID != versionID else { return .unchanged }
        guard capturedViewIDs.isEmpty else { return .needsConfirmation }
        applySelection(versionID)
        return .changed
    }

    func confirmSelection(_ versionID: String) {
        guard candidates.contains(where: { $0.id == versionID }) else { return }
        applySelection(versionID)
    }

    func beginCandidateLoad() -> SOPCandidateLoadToken {
        candidateLoadGeneration += 1
        isCandidateLoading = true
        candidateLoadFailed = false
        return SOPCandidateLoadToken(generation: candidateLoadGeneration)
    }

    @discardableResult
    func acceptCandidates(_ summaries: [CaptureSOPSummary], token: SOPCandidateLoadToken) -> Bool {
        guard token.generation == candidateLoadGeneration else { return false }
        installCandidates(publishedVersionCandidates(from: summaries))
        candidateLoadFailed = false
        return true
    }

    @discardableResult
    func failCandidateLoad(token: SOPCandidateLoadToken) -> Bool {
        guard token.generation == candidateLoadGeneration else { return false }
        candidateLoadFailed = true
        return true
    }

    @discardableResult
    func finishCandidateLoad(token: SOPCandidateLoadToken) -> Bool {
        guard token.generation == candidateLoadGeneration else { return false }
        isCandidateLoading = false
        return true
    }

    func beginVersionLoad() -> SOPVersionLoadToken? {
        guard let selectedVersionID else { return nil }
        loadGeneration += 1
        version = nil
        return SOPVersionLoadToken(versionID: selectedVersionID, generation: loadGeneration)
    }

    @discardableResult
    func accept(version: SOPVersion, token: SOPVersionLoadToken) -> SOPVersionAcceptance {
        guard token.generation == loadGeneration,
              token.versionID == selectedVersionID else { return .stale }
        guard version.id == selectedVersionID else { return .invalidResponse }
        self.version = version
        return .accepted
    }

    func recordCapture(viewID: String) {
        capturedViewIDs.insert(viewID)
    }

    func resolveSession(
        create: @MainActor (String) async throws -> PhotoSession
    ) async throws -> PhotoSession {
        if let session { return session }
        guard let selectedVersionID else { throw APIError.invalidResponse }
        if sessionCreationVersionID == selectedVersionID {
            return try await withCheckedThrowingContinuation { continuation in
                sessionWaiters.append(continuation)
            }
        }

        sessionCreationVersionID = selectedVersionID
        do {
            let created = try await create(selectedVersionID)
            guard self.selectedVersionID == selectedVersionID,
                  sessionCreationVersionID == selectedVersionID else { throw CancellationError() }
            session = created
            finishSessionCreation(with: .success(created))
            return created
        } catch {
            if sessionCreationVersionID == selectedVersionID {
                finishSessionCreation(with: .failure(error))
            }
            throw error
        }
    }

    private func applySelection(_ versionID: String?) {
        if sessionCreationVersionID != nil {
            finishSessionCreation(with: .failure(CancellationError()))
        }
        loadGeneration += 1
        selectedVersionID = versionID
        version = nil
        capturedViewIDs = []
        session = nil
    }

    private func finishSessionCreation(with result: Result<PhotoSession, Error>) {
        let waiters = sessionWaiters
        sessionWaiters = []
        sessionCreationVersionID = nil
        for waiter in waiters {
            switch result {
            case .success(let session): waiter.resume(returning: session)
            case .failure(let error): waiter.resume(throwing: error)
            }
        }
    }
}

struct SOPCaptureView: View {
    let sku: SKU

    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject private var language: LanguageStore
    @StateObject private var captureState = SOPCaptureState()
    @State private var capturedImages: [String: UIImage] = [:]
    @State private var selectedView: SOPView?
    @State private var pendingVersionID: String?
    @State private var isSourcePickerPresented = false
    @State private var uploadingViewID: String?
    @State private var isVersionLoading = false
    @State private var versionError = false
    @State private var operationFailure: CaptureOperationFailure?

    private var views: [SOPView] {
        captureState.version?.views.sorted(by: { $0.sequence < $1.sequence }) ?? []
    }

    private var canFinish: Bool {
        captureState.version != nil
            && requiredViewsComplete(views: views, capturedViewIDs: captureState.capturedViewIDs)
    }

    var body: some View {
        NavigationStack {
            Group {
                if captureState.isCandidateLoading {
                    loadingView(key: "capture.loading.sops")
                } else if captureState.candidateLoadFailed {
                    unavailableView(
                        titleKey: "capture.sops.failed",
                        descriptionKey: "capture.sops.failed.desc",
                        retry: { Task { await loadCandidates() } }
                    )
                } else if captureState.candidates.isEmpty {
                    unavailableView(
                        titleKey: "capture.no.published.sop",
                        descriptionKey: "capture.no.published.sop.desc",
                        retry: { Task { await loadCandidates() } }
                    )
                } else {
                    captureList
                }
            }
            .navigationTitle(language.t("sop.capture"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(language.t("close")) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(language.t("finish")) { dismiss() }
                        .disabled(!canFinish || uploadingViewID != nil)
                }
            }
            .task { await loadCandidates() }
            .task(id: captureState.selectedVersionID) {
                guard captureState.selectedVersionID != nil else { return }
                await loadSelectedVersion()
            }
            .sheet(isPresented: $isSourcePickerPresented) {
                CaptureSourceView(view: selectedView) { image in
                    guard let selectedView else { return }
                    isSourcePickerPresented = false
                    Task { await upload(image, for: selectedView) }
                }
            }
            .alert(language.t("capture.change.version.title"), isPresented: versionConfirmationBinding) {
                Button(language.t("cancel"), role: .cancel) { pendingVersionID = nil }
                Button(language.t("capture.change.version"), role: .destructive) {
                    guard let pendingVersionID else { return }
                    capturedImages = [:]
                    selectedView = nil
                    captureState.confirmSelection(pendingVersionID)
                    self.pendingVersionID = nil
                }
            } message: {
                Text(language.t("capture.change.version.desc"))
            }
            .alert(item: $operationFailure) { failure in
                Alert(
                    title: Text(language.t(failure.titleKey)),
                    message: Text(language.t(failure.descriptionKey)),
                    dismissButton: .default(Text(language.t("close")))
                )
            }
        }
    }

    private var captureList: some View {
        List {
            Section(language.t("sku.section")) {
                LabeledContent(language.t("code"), value: sku.code)
                LabeledContent(language.t("product"), value: sku.product.name)
            }

            Section(language.t("capture.sop.version")) {
                if captureState.candidates.count > 1 {
                    Picker(language.t("capture.select.version"), selection: versionSelectionBinding) {
                        ForEach(captureState.candidates) { candidate in
                            Text(versionLabel(candidate)).tag(candidate.id)
                        }
                    }
                    .disabled(uploadingViewID != nil)
                    .accessibilityHint(language.t("capture.select.version.hint"))
                } else if let candidate = captureState.candidates.first {
                    LabeledContent(language.t("capture.version"), value: versionLabel(candidate))
                }
            }

            if isVersionLoading {
                Section { loadingRow(key: "capture.loading.version") }
            } else if versionError {
                Section {
                    VStack(alignment: .leading, spacing: 12) {
                        Label(language.t("capture.version.failed"), systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.primary)
                        Text(language.t("capture.version.failed.desc"))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                        Button(language.t("retry")) { Task { await loadSelectedVersion() } }
                            .buttonStyle(.bordered)
                            .frame(minHeight: 44)
                    }
                    .padding(.vertical, 8)
                }
            } else if captureState.version != nil {
                Section {
                    ForEach(views) { view in
                        Button {
                            selectedView = view
                            isSourcePickerPresented = true
                        } label: {
                            CaptureViewRow(
                                view: view,
                                image: capturedImages[view.id],
                                isUploading: uploadingViewID == view.id,
                                language: language
                            )
                        }
                        .buttonStyle(.plain)
                        .disabled(uploadingViewID != nil)
                        .frame(minHeight: 72)
                    }
                } header: {
                    Text(language.t("view.checklist"))
                } footer: {
                    Text(language.t("capture.required.footer"))
                }
            }
        }
    }

    private var versionSelectionBinding: Binding<String> {
        Binding(
            get: { captureState.selectedVersionID ?? captureState.candidates[0].id },
            set: { newID in
                switch captureState.requestSelection(newID) {
                case .needsConfirmation:
                    pendingVersionID = newID
                case .changed:
                    capturedImages = [:]
                    selectedView = nil
                case .unchanged:
                    break
                }
            }
        )
    }

    private var versionConfirmationBinding: Binding<Bool> {
        Binding(
            get: { pendingVersionID != nil },
            set: { if !$0 { pendingVersionID = nil } }
        )
    }

    private func versionLabel(_ candidate: SOPVersionCandidate) -> String {
        "\(candidate.name.value(for: language.language)) · V\(candidate.versionNumber)"
    }

    private func loadingView(key: String) -> some View {
        VStack(spacing: 12) {
            ProgressView()
            Text(language.t(key)).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }

    private func loadingRow(key: String) -> some View {
        HStack(spacing: 12) {
            ProgressView()
            Text(language.t(key)).foregroundStyle(.secondary)
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }

    private func unavailableView(
        titleKey: String,
        descriptionKey: String,
        retry: @escaping () -> Void
    ) -> some View {
        ContentUnavailableView {
            Label(language.t(titleKey), systemImage: "camera.viewfinder")
        } description: {
            Text(language.t(descriptionKey))
        } actions: {
            Button(language.t("retry"), action: retry)
                .buttonStyle(.borderedProminent)
                .frame(minHeight: 44)
        }
    }

    @MainActor
    private func loadCandidates() async {
        let token = captureState.beginCandidateLoad()
        versionError = false
        defer { captureState.finishCandidateLoad(token: token) }

        guard let categoryID = sku.product.catalogCategory?.id else {
            _ = captureState.acceptCandidates([], token: token)
            return
        }
        do {
            let summaries = try await APIClient.shared.listPublishedSOPs(categoryID: categoryID).data
            guard !Task.isCancelled else { return }
            _ = captureState.acceptCandidates(summaries, token: token)
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled else { return }
            _ = captureState.failCandidateLoad(token: token)
        }
    }

    @MainActor
    private func loadSelectedVersion() async {
        guard let token = captureState.beginVersionLoad() else { return }
        isVersionLoading = true
        versionError = false
        defer { isVersionLoading = false }
        do {
            let version = try await APIClient.shared.getSOPVersion(id: token.versionID)
            guard !Task.isCancelled else { return }
            switch captureState.accept(version: version, token: token) {
            case .accepted, .stale:
                break
            case .invalidResponse:
                versionError = true
            }
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled, token.versionID == captureState.selectedVersionID else { return }
            versionError = true
        }
    }

    @MainActor
    private func upload(_ image: UIImage, for view: SOPView) async {
        guard captureState.version?.views.contains(where: { $0.id == view.id }) == true,
              let imageData = image.jpegData(compressionQuality: 0.82) else {
            operationFailure = .upload
            return
        }

        uploadingViewID = view.id
        defer { uploadingViewID = nil }

        let session: PhotoSession
        do {
            session = try await captureState.resolveSession { versionID in
                try await APIClient.shared.createPhotoSession(skuID: sku.id, sopVersionID: versionID)
            }
        } catch is CancellationError {
            return
        } catch {
            operationFailure = classifyCaptureFailure(error, during: .sessionCreation)
            return
        }

        do {
            let fileName = "view-\(view.id)-\(UUID().uuidString).jpg"
            _ = try await APIClient.shared.uploadImage(
                imageData,
                skuID: sku.id,
                sopViewID: view.id,
                photoSessionID: session.id,
                fileName: fileName
            )
            capturedImages[view.id] = image
            captureState.recordCapture(viewID: view.id)
        } catch is CancellationError {
            return
        } catch {
            operationFailure = classifyCaptureFailure(error, during: .upload)
        }
    }
}

enum CaptureOperationStage {
    case sessionCreation
    case upload
}

enum CaptureOperationFailure: String, Identifiable, Equatable {
    case session
    case upload

    var id: String { rawValue }
    var titleKey: String { self == .session ? "capture.session.failed" : "capture.upload.failed" }
    var descriptionKey: String { self == .session ? "capture.session.failed.desc" : "capture.upload.failed.desc" }
}

func classifyCaptureFailure(_ error: Error, during stage: CaptureOperationStage) -> CaptureOperationFailure {
    _ = error
    switch stage {
    case .sessionCreation: return .session
    case .upload: return .upload
    }
}

func shotRowAccessibilityLabel(
    view: SOPView,
    language: AppLanguage,
    kind: String,
    requirement: String
) -> String {
    [
        String(format: "%02d", view.sequence),
        view.displayName(for: language),
        view.displayInstruction(for: language),
        kind,
        requirement,
    ]
    .filter { !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    .joined(separator: ", ")
}

private struct CaptureViewRow: View {
    let view: SOPView
    let image: UIImage?
    let isUploading: Bool
    let language: LanguageStore

    private var kindKey: String {
        if view.role == "reference_front" { return "capture.view.reference" }
        return view.viewKind == "detail" ? "capture.view.detail" : "capture.view.standard"
    }

    private var stateKey: String {
        if isUploading { return "capture.state.uploading" }
        return image == nil ? "capture.state.pending" : "capture.state.complete"
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Text(String(format: "%02d", view.sequence))
                .font(.headline.monospacedDigit())
                .foregroundStyle(.secondary)
                .frame(width: 30, alignment: .leading)
                .accessibilityHidden(true)

            VStack(alignment: .leading, spacing: 5) {
                Text(view.displayName(for: language.language))
                    .font(.headline)
                    .foregroundStyle(.primary)
                Text(view.displayInstruction(for: language.language))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Text("\(language.t(kindKey)) · \(language.t(view.required ? "required" : "optional"))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer(minLength: 8)

            VStack(spacing: 5) {
                if isUploading {
                    ProgressView()
                        .frame(width: 44, height: 44)
                } else if let image {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFill()
                        .frame(width: 52, height: 52)
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                        .overlay(alignment: .bottomTrailing) {
                            Image(systemName: "checkmark.circle.fill")
                                .symbolRenderingMode(.palette)
                                .foregroundStyle(.white, .green)
                                .background(.background, in: Circle())
                        }
                        .accessibilityHidden(true)
                } else {
                    Image(systemName: "camera.viewfinder")
                        .frame(width: 52, height: 52)
                        .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                }
                Text(language.t(stateKey))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            shotRowAccessibilityLabel(
                view: view,
                language: language.language,
                kind: language.t(kindKey),
                requirement: language.t(view.required ? "required" : "optional")
            )
        )
        .accessibilityValue(language.t(stateKey))
        .accessibilityHint(language.t("capture.row.hint"))
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
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(view?.displayName(for: language.language) ?? language.t("view"))
                            .font(.title2.bold())
                            .fixedSize(horizontal: false, vertical: true)
                        Text(view?.displayInstruction(for: language.language) ?? "")
                            .font(.body)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    if !UIImagePickerController.isSourceTypeAvailable(.camera) {
                        Text(language.t("capture.camera.unavailable"))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    if isReadingPhoto {
                        HStack(spacing: 10) {
                            ProgressView()
                            Text(language.t("capture.preparing")).foregroundStyle(.secondary)
                        }
                        .accessibilityElement(children: .combine)
                    }

                    if let errorKey {
                        Label(language.t(errorKey), systemImage: "exclamationmark.triangle")
                            .font(.subheadline)
                            .foregroundStyle(.red)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(24)
            }
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: 12) {
                    Button {
                        isCameraPresented = true
                    } label: {
                        Label(language.t("capture.camera"), systemImage: "camera.fill")
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera) || isReadingPhoto)

                    PhotosPicker(selection: $selectedPhoto, matching: .images) {
                        Label(photoLibraryTitle, systemImage: "photo.on.rectangle")
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                    .buttonStyle(.bordered)
                    .disabled(isReadingPhoto)
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 12)
                .background(.bar)
            }
            .navigationTitle(language.t("capture"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(language.t("cancel")) { dismiss() }
                }
            }
            .onChange(of: selectedPhoto) { _, item in
                guard let item else { return }
                Task { await loadPhoto(item) }
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

    @MainActor
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
        } catch is CancellationError {
            return
        } catch {
            errorKey = "capture.image.invalid"
        }
    }
}

private struct CameraPicker: UIViewControllerRepresentable {
    let onComplete: (UIImage?) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onComplete: onComplete) }

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

        init(onComplete: @escaping (UIImage?) -> Void) { self.onComplete = onComplete }

        func imagePickerController(
            _ picker: UIImagePickerController,
            didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            picker.dismiss(animated: true) { self.onComplete(info[.originalImage] as? UIImage) }
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            picker.dismiss(animated: true) { self.onComplete(nil) }
        }
    }
}
