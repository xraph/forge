import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:sqlite3/common.dart';

/// DatabaseFiles whose delete fails like a crash halfway through sign-out.
final class InterruptedDeleteFiles implements DatabaseFiles {
  InterruptedDeleteFiles(this.inner);

  final DatabaseFiles inner;
  bool interrupt = true;

  /// Runs as delete starts, before anything is deleted or interrupted.
  void Function()? beforeDelete;

  @override
  Future<CommonDatabase> open(String principal) => inner.open(principal);

  @override
  String kind(String principal) => inner.kind(principal);

  @override
  Future<void> afterWrite(String principal) => inner.afterWrite(principal);

  @override
  Future<void> close(String principal) => inner.close(principal);

  @override
  Future<void> delete(String principal) async {
    beforeDelete?.call();
    if (interrupt) {
      await inner.close(principal);
      throw StateError('simulated crash while deleting the database file');
    }
    await inner.delete(principal);
  }

  @override
  Future<void> deleteAll() => inner.deleteAll();
}
