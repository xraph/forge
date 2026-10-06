// ignore_for_file: avoid_print
import 'package:forge_client/forge_client.dart';

final class _Quiet implements Transport {
  @override
  Future<Object?> execute(TransportRequest request) async => null;
}

void main() {
  final cache = configureClient(transport: _Quiet(), entities: const {});
  print('forge-release-fixture-marker ${cache.principal}');
}
